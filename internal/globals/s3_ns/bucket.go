package s3_ns

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	core "github.com/inoxlang/inox/internal/core"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

var (
	ErrCannotResolveBucket = errors.New("cannot resolve bucket")

	//ResourceName -> Bucket
	openBucketMap     = make(map[string]BucketMapItem)
	openBucketMapLock sync.RWMutex
)

type BucketMapItem = struct {
	WithCredentials, Public *Bucket
}

type Bucket struct {
	closed uint32

	s3Host core.Host
	name   string

	client      *S3Client
	fakeBackend gofakes3.Backend
}

func (b *Bucket) Close() {
	if !atomic.CompareAndSwapUint32(&b.closed, 0, 1) {
		return
	}
}

func (b *Bucket) RemoveAllObjects(ctx context.Context) {
	objectChan := b.client.libClient.ListObjects(ctx, b.name, minio.ListObjectsOptions{Recursive: true})
	for range b.client.libClient.RemoveObjects(ctx, b.name, objectChan, minio.RemoveObjectsOptions{}) {
	}
}

func OpenBucket(ctx *core.Context, s3Host core.Host) (*Bucket, error) {

	switch s3Host.Scheme() {
	case "https":
		return openPublicBucket(ctx, s3Host, s3Host)
	case "s3":
	default:
		return nil, ErrCannotResolveBucket
	}

	data := ctx.GetHostResolutionData(s3Host)

	switch d := data.(type) {
	case core.Host:
		switch d.Scheme() {
		case "https":
			return openPublicBucket(ctx, s3Host, d)
		}
		return nil, ErrCannotResolveBucket
	case core.URL:
		switch d.Scheme() {
		case "mem":
			return openInMemoryBucket(ctx, s3Host, d)
		}
		return nil, ErrCannotResolveBucket
	case *core.Object:
		bucket := d.Prop(ctx, "bucket")
		if bucket == nil {
			return nil, fmt.Errorf("%w: missing .bucket in resolution data", ErrCannotResolveBucket)
		}

		host := d.Prop(ctx, "host")
		if host == nil {
			return nil, fmt.Errorf("%w: missing .host in resolution data", ErrCannotResolveBucket)
		}

		provider := d.Prop(ctx, "provider")
		if bucket == nil {
			return nil, fmt.Errorf("%w: missing .provider in resolution data", ErrCannotResolveBucket)
		}

		accessKey := d.Prop(ctx, "access-key")
		if accessKey == nil {
			return nil, fmt.Errorf("%w: missing .access-key in resolution data", ErrCannotResolveBucket)
		}

		secretKey := d.Prop(ctx, "secret-key")

		if secretKey == nil {
			return nil, fmt.Errorf("%w: missing .secret-key in resolution data", ErrCannotResolveBucket)
		}

		return openBucketWithCredentials(ctx, openBucketWithCredentialsInput{
			s3Host:     s3Host,
			httpsHost:  host.(core.Host),
			bucketName: bucket.(core.StringLike).GetOrBuildString(),
			provider:   provider.(core.StringLike).GetOrBuildString(),
			accessKey:  accessKey.(core.StringLike).GetOrBuildString(),
			secretKey:  secretKey.(core.StringLike).GetOrBuildString(),
		})
	default:
		return nil, ErrCannotResolveBucket
	}
}

func openInMemoryBucket(ctx *core.Context, s3Host core.Host, memURL core.URL) (*Bucket, error) {
	openBucketMapLock.RLock()
	item, ok := openBucketMap[string(memURL)]
	openBucketMapLock.RUnlock()

	if ok && item.WithCredentials != nil {
		return item.WithCredentials, nil
	}

	backend := s3mem.New()
	if err := backend.CreateBucket(string(memURL.Host())); err != nil {
		return nil, err
	}

	bucket := &Bucket{
		s3Host:      s3Host,
		name:        string(memURL.Host()),
		fakeBackend: backend,
	}

	openBucketMapLock.Lock()
	openBucketMap[string(memURL)] = BucketMapItem{WithCredentials: bucket}
	openBucketMapLock.Unlock()

	return bucket, nil
}

func openPublicBucket(ctx *core.Context, s3Host core.Host, httpsHost core.Host) (*Bucket, error) {
	_host := string(httpsHost)

	openBucketMapLock.RLock()
	item, ok := openBucketMap[_host]
	openBucketMapLock.RUnlock()

	if ok && item.Public != nil {
		return item.Public, nil
	}

	subdomain, endpoint, _ := strings.Cut(httpsHost.WithoutScheme(), ".")

	s3Client, err := minio.New(endpoint, &minio.Options{
		Secure: true,
	})

	if err != nil {
		return nil, err
	}

	bucket := &Bucket{
		s3Host: s3Host,
		name:   subdomain,
		client: &S3Client{libClient: s3Client},
	}

	item.Public = bucket
	openBucketMapLock.Lock()
	openBucketMap[_host] = item
	openBucketMapLock.Unlock()

	return bucket, nil
}

type openBucketWithCredentialsInput struct {
	s3Host core.Host

	provider   string
	httpsHost  core.Host
	bucketName string

	accessKey, secretKey string
}

func openBucketWithCredentials(ctx *core.Context, input openBucketWithCredentialsInput) (*Bucket, error) {
	_host := string(input.httpsHost)
	openBucketMapLock.RLock()
	item, ok := openBucketMap[_host]
	openBucketMapLock.RUnlock()

	if ok && item.WithCredentials != nil {
		return item.WithCredentials, nil
	}

	var endpoint string
	var region string
	var lookup minio.BucketLookupType

	if input.httpsHost.Scheme() != "https" {
		return nil, fmt.Errorf("bucket endpoint should have a https:// scheme")
	}

	switch strings.ToLower(input.provider) {
	case "cloudflare":
		endpoint = input.httpsHost.WithoutScheme()
		lookup = minio.BucketLookupPath
		region = "auto"
	default:
		return nil, fmt.Errorf("S3 provider %q is not supported", input.provider)
	}

	s3Client, err := minio.New(endpoint, &minio.Options{
		Region:       region,
		Creds:        credentials.NewStaticV4(input.accessKey, input.secretKey, ""),
		Secure:       true,
		BucketLookup: lookup,
	})

	if err != nil {
		return nil, err
	}

	bucket := &Bucket{
		s3Host: input.s3Host,
		name:   input.bucketName,
		client: &S3Client{libClient: s3Client},
	}

	item.WithCredentials = bucket
	openBucketMapLock.Lock()
	openBucketMap[_host] = item
	openBucketMapLock.Unlock()

	return bucket, nil
}
