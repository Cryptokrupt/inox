package project_server

import (
	"github.com/inoxlang/inox/internal/parse"
	"github.com/inoxlang/inox/internal/project_server/lsp/defines"
)

func rangeToLspRange(r parse.SourcePositionRange) defines.Range {
	return defines.Range{
		Start: defines.Position{
			Line:      uint(r.StartLine) - 1,
			Character: uint(r.StartColumn - 1),
		},
		//exclusive end
		End: defines.Position{
			Line:      uint(r.EndLine - 1),
			Character: uint(r.EndColumn - 1),
		},
	}
}

func firstCharsLspRange(count int32) defines.Range {
	return rangeToLspRange(parse.SourcePositionRange{
		StartLine:   1,
		StartColumn: 1,
		EndLine:     1,
		EndColumn:   1,
		Span:        parse.NodeSpan{Start: 0, End: count},
	})
}

func getPositionInPositionStackOrFirst(positions parse.SourcePositionStack, fpath string) parse.SourcePositionRange {
	for _, pos := range positions {
		if pos.SourceName == fpath {
			return pos
		}
	}

	return positions[0]
}

func getLineColumn(pos defines.Position) (int32, int32) {
	line := int32(pos.Line + 1)
	column := int32(pos.Character + 1)
	return line, column
}
