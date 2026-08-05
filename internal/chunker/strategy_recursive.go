package chunker

import (
	"strings"
)

type recursiveStrategy struct{}

func NewRecursiveStrategy() Strategy {
	return &recursiveStrategy{}
}

var defaultSeparators = []string{"\n\n", "\n", "。", "！", "？", ".", "!", "?", " ", ""}

func (s *recursiveStrategy) Split(text string, config ChunkerConfig, tokenizer Tokenizer) []string {
	if text == "" {
		return nil
	}

	if tokenizer.Count(text) <= config.ChunkSize {
		return []string{text}
	}

	segments := splitRecursive(text, defaultSeparators, config.ChunkSize, tokenizer)
	merged := mergeSegments(segments, config.ChunkSize, tokenizer)

	if config.ChunkOverlap > 0 && len(merged) > 1 {
		merged = addOverlap(merged, config.ChunkOverlap, tokenizer)
	}

	return merged
}

func splitRecursive(text string, separators []string, chunkSize int, tokenizer Tokenizer) []string {
	if tokenizer.Count(text) <= chunkSize {
		return []string{text}
	}

	if len(separators) == 0 {
		// 最后手段：按字符硬切
		return hardSplit(text, chunkSize, tokenizer)
	}

	sep := separators[0]
	remaining := separators[1:]

	var parts []string
	if sep == "" {
		return hardSplit(text, chunkSize, tokenizer)
	}

	splits := strings.Split(text, sep)
	for _, part := range splits {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if tokenizer.Count(part) <= chunkSize {
			parts = append(parts, part)
		} else {
			subParts := splitRecursive(part, remaining, chunkSize, tokenizer)
			parts = append(parts, subParts...)
		}
	}

	return parts
}

func mergeSegments(segments []string, chunkSize int, tokenizer Tokenizer) []string {
	if len(segments) == 0 {
		return nil
	}

	var merged []string
	var current strings.Builder

	for _, seg := range segments {
		if current.Len() == 0 {
			current.WriteString(seg)
			continue
		}

		combined := current.String() + "\n\n" + seg
		if tokenizer.Count(combined) <= chunkSize {
			current.WriteString("\n\n")
			current.WriteString(seg)
		} else {
			merged = append(merged, strings.TrimSpace(current.String()))
			current.Reset()
			current.WriteString(seg)
		}
	}

	if current.Len() > 0 {
		merged = append(merged, strings.TrimSpace(current.String()))
	}

	return merged
}

func addOverlap(chunks []string, overlapTokens int, tokenizer Tokenizer) []string {
	if len(chunks) <= 1 {
		return chunks
	}

	result := make([]string, len(chunks))
	result[0] = chunks[0]

	for i := 1; i < len(chunks); i++ {
		prevRunes := []rune(chunks[i-1])
		overlap := extractOverlapSuffix(prevRunes, overlapTokens, tokenizer)
		if overlap != "" {
			result[i] = overlap + "\n" + chunks[i]
		} else {
			result[i] = chunks[i]
		}
	}

	return result
}

func extractOverlapSuffix(runes []rune, overlapTokens int, tokenizer Tokenizer) string {
	for i := len(runes) - 1; i >= 0; i-- {
		segment := string(runes[i:])
		if tokenizer.Count(segment) >= overlapTokens {
			// 尝试在空白处对齐
			for j := i; j < len(runes); j++ {
				if runes[j] == ' ' || runes[j] == '\n' {
					return strings.TrimSpace(string(runes[j+1:]))
				}
			}
			return strings.TrimSpace(segment)
		}
	}
	return strings.TrimSpace(string(runes))
}

func hardSplit(text string, chunkSize int, tokenizer Tokenizer) []string {
	runes := []rune(text)
	var result []string
	start := 0

	for start < len(runes) {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}

		// 收缩到 token 限制内
		for end > start+1 && tokenizer.Count(string(runes[start:end])) > chunkSize {
			end--
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			result = append(result, chunk)
		}
		start = end
	}

	return result
}
