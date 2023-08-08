package core

import (
	"path/filepath"
	"strconv"

	"github.com/inoxlang/inox/internal/core/symbolic"
	"github.com/inoxlang/inox/internal/utils"
)

// TODO: improve name
type MigrationAwarePattern interface {
	Pattern
	GetMigrationOperations(ctx *Context, next Pattern, pseudoPath string) ([]MigrationOp, error)
}

type MigrationOp interface {
	PseudPath()
}

type migrationMixin struct {
	pseudoPath string
}

func (m migrationMixin) PseudPath() {

}

type ReplacementMigrationOp struct {
	Current, Next Pattern
	migrationMixin
}

type RemovalMigrationOp struct {
	Value Pattern
	migrationMixin
}

type NillableInitializationMigrationOp struct {
	Value Pattern
	migrationMixin
}

type InclusionMigrationOp struct {
	Value    Pattern
	Optional bool
	migrationMixin
}

func (patt *ObjectPattern) GetMigrationOperations(ctx *Context, next Pattern, pseudoPath string) (migrations []MigrationOp, _ error) {
	nextObject, ok := next.(*ObjectPattern)
	if !ok {
		return []MigrationOp{ReplacementMigrationOp{Current: patt, Next: next, migrationMixin: migrationMixin{pseudoPath: pseudoPath}}}, nil
	}

	if patt.entryPatterns == nil {
		return []MigrationOp{ReplacementMigrationOp{Current: patt, Next: next, migrationMixin: migrationMixin{pseudoPath: pseudoPath}}}, nil
	}

	if nextObject.entryPatterns == nil {
		return nil, nil
	}

	for propName, propPattern := range patt.entryPatterns {
		propPseudoPath := filepath.Join(pseudoPath, propName)
		_, isOptional := patt.optionalEntries[propName]
		_, isOptionalInOther := nextObject.optionalEntries[propName]

		nextPropPattern, presentInOther := nextObject.entryPatterns[propName]

		if !presentInOther {
			migrations = append(migrations, RemovalMigrationOp{
				Value:          propPattern,
				migrationMixin: migrationMixin{propPseudoPath},
			})
			continue
		}

		list, err := GetMigrationOperations(ctx, propPattern, nextPropPattern, propPseudoPath)
		if err != nil {
			return nil, err
		}

		if len(list) == 0 && isOptional && !isOptionalInOther {
			list = append(list, NillableInitializationMigrationOp{
				Value:          propPattern,
				migrationMixin: migrationMixin{propPseudoPath},
			})
		}
		migrations = append(migrations, list...)
	}

	for propName, nextPropPattern := range nextObject.entryPatterns {
		_, presentInCurrent := patt.entryPatterns[propName]

		if presentInCurrent {
			//already handled
			continue
		}
		propPseudoPath := filepath.Join(pseudoPath, propName)
		_, isOptional := nextObject.optionalEntries[propName]

		migrations = append(migrations, InclusionMigrationOp{
			Value:          nextPropPattern,
			Optional:       isOptional,
			migrationMixin: migrationMixin{propPseudoPath},
		})
	}

	return migrations, nil
}

func (patt *RecordPattern) GetMigrationOperations(ctx *Context, next Pattern, pseudoPath string) (migrations []MigrationOp, _ error) {
	nextRecord, ok := next.(*RecordPattern)
	if !ok {
		return []MigrationOp{ReplacementMigrationOp{Current: patt, Next: next, migrationMixin: migrationMixin{pseudoPath: pseudoPath}}}, nil
	}

	if patt.entryPatterns == nil {
		return []MigrationOp{ReplacementMigrationOp{Current: patt, Next: next, migrationMixin: migrationMixin{pseudoPath: pseudoPath}}}, nil
	}

	if nextRecord.entryPatterns == nil {
		return nil, nil
	}

	for propName, propPattern := range patt.entryPatterns {
		propPseudoPath := filepath.Join(pseudoPath, propName)
		_, isOptional := patt.optionalEntries[propName]
		_, isOptionalInOther := nextRecord.optionalEntries[propName]

		nextPropPattern, presentInOther := nextRecord.entryPatterns[propName]

		if !presentInOther {
			migrations = append(migrations, RemovalMigrationOp{
				Value:          propPattern,
				migrationMixin: migrationMixin{propPseudoPath},
			})
			continue
		}

		list, err := GetMigrationOperations(ctx, propPattern, nextPropPattern, propPseudoPath)
		if err != nil {
			return nil, err
		}

		if len(list) == 0 && isOptional && !isOptionalInOther {
			list = append(list, NillableInitializationMigrationOp{
				Value:          propPattern,
				migrationMixin: migrationMixin{propPseudoPath},
			})
		}
		migrations = append(migrations, list...)
	}

	for propName, nextPropPattern := range nextRecord.entryPatterns {
		_, presentInCurrent := patt.entryPatterns[propName]

		if presentInCurrent {
			//already handled
			continue
		}
		propPseudoPath := filepath.Join(pseudoPath, propName)
		_, isOptional := nextRecord.optionalEntries[propName]

		migrations = append(migrations, InclusionMigrationOp{
			Value:          nextPropPattern,
			Optional:       isOptional,
			migrationMixin: migrationMixin{propPseudoPath},
		})
	}

	return migrations, nil
}

func (patt *ListPattern) GetMigrationOperations(ctx *Context, next Pattern, pseudoPath string) (migrations []MigrationOp, _ error) {
	nextList, ok := next.(*ListPattern)
	if !ok {
		return []MigrationOp{ReplacementMigrationOp{Current: patt, Next: next, migrationMixin: migrationMixin{pseudoPath: pseudoPath}}}, nil
	}

	anyElemPseudoPath := filepath.Join(pseudoPath, "*")

	if patt.generalElementPattern != nil {
		if nextList.generalElementPattern != nil {
			return GetMigrationOperations(ctx, patt.generalElementPattern, nextList.generalElementPattern, anyElemPseudoPath)
		}
		return []MigrationOp{ReplacementMigrationOp{Current: patt, Next: next, migrationMixin: migrationMixin{pseudoPath: pseudoPath}}}, nil
	}
	//else pattern has element patterns

	if nextList.generalElementPattern != nil {
		//add operation for each current element that does not match the next general element pattern
		for i, currentElemPattern := range patt.elementPatterns {
			elemPseudoPath := filepath.Join(pseudoPath, strconv.Itoa(i))

			list, err := GetMigrationOperations(ctx, currentElemPattern, nextList.generalElementPattern, elemPseudoPath)
			if err != nil {
				return nil, err
			}
			migrations = append(migrations, list...)
		}
	} else {
		if len(nextList.elementPatterns) != len(patt.elementPatterns) {
			return []MigrationOp{ReplacementMigrationOp{Current: patt, Next: next, migrationMixin: migrationMixin{pseudoPath: pseudoPath}}}, nil
		}

		//add operation for each current element that does not match the element pattern at the corresponding index
		for i, nextElemPattern := range nextList.elementPatterns {
			elemPseudoPath := filepath.Join(pseudoPath, strconv.Itoa(i))
			currentElemPattern := patt.elementPatterns[i]

			list, err := GetMigrationOperations(ctx, currentElemPattern, nextElemPattern, elemPseudoPath)
			if err != nil {
				return nil, err
			}
			migrations = append(migrations, list...)
		}
	}

	return migrations, nil
}

func GetMigrationOperations(ctx *Context, current, next Pattern, pseudoPath string) ([]MigrationOp, error) {
	if current == next || isSubType(current, next, ctx, map[uintptr]symbolic.SymbolicValue{}) {
		return nil, nil
	}

	m1, ok := current.(MigrationAwarePattern)

	if !ok {
		return []MigrationOp{ReplacementMigrationOp{Current: current, Next: next, migrationMixin: migrationMixin{pseudoPath: pseudoPath}}}, nil
	}

	return m1.GetMigrationOperations(ctx, next, pseudoPath)
}

func isSubType(sub, super Pattern, ctx *Context, encountered map[uintptr]symbolic.SymbolicValue) bool {
	symbolicSub := utils.Must(sub.ToSymbolicValue(ctx, encountered))
	symbolicSuper := utils.Must(super.ToSymbolicValue(ctx, encountered))

	if !symbolic.IsConcretizable(symbolicSub) {
		panic(ErrUnreachable)
	}

	if !symbolic.IsConcretizable(symbolicSuper) {
		panic(ErrUnreachable)
	}

	return symbolicSuper.Test(symbolicSub)
}

func isSuperType(super, sub Pattern, ctx *Context, encountered map[uintptr]symbolic.SymbolicValue) bool {
	return isSubType(sub, super, ctx, encountered)
}
