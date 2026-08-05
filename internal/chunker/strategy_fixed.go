package chunker

import (
	"strings"
	"unicode/utf8"
)

type fixedSizeStrategy struct{}

func NewFixedSizeStrategy() Strategy {
	return &fixedSizeStrategy{}
}

func (s *fixedSizeStrategy) Split(text string, config ChunkerConfig, tokenizer Tokenizer) []string {
	if text == "" {
		return nil
	}

	totalTokens := tokenizer.Count(text)
	if totalTokens <= config.ChunkSize {
		return []string{text}
	}

	var chunks []string
	runes := []rune(text)
	start := 0

	for start < len(runes) {
		end := findChunkEnd(runes, start, config.ChunkSize, tokenizer)
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		if end >= len(runes) {
			break
		}

		// overlap：回退 overlap token 对应的字符数
		overlapStart := findOverlapStart(runes, end, config.ChunkOverlap, tokenizer)
		if overlapStart <= start {
			start = end
		} else {
			start = overlapStart
		}
	}

	return chunks
}

func findChunkEnd(runes []rune, start, chunkSize int, tokenizer Tokenizer) int {
	if start >= len(runes) {
		return len(runes)
	}

	// 二分逼近：先估算结束位置，再精确调整
	estimatedEnd := start + chunkSize
	if estimatedEnd >= len(runes) {
		// 检查剩余是否在限制内
		if tokenizer.Count(string(runes[start:])) <= chunkSize {
			return len(runes)
		}
		estimatedEnd = len(runes) - 1
	}

	// 向前收缩直到满足 token 限制
	end := estimatedEnd
	for end > start && tokenizer.Count(string(runes[start:end])) > chunkSize {
		end -= max(1, (end-start)/10)
	}

	// 向后扩展直到刚好超限
	for end < len(runes) && tokenizer.Count(string(runes[start:end+1])) <= chunkSize {
		end++
	}

	// 回退到最近的空白字符位置
	if end < len(runes) {
		breakPoint := findBreakPoint(runes, start, end)
		if breakPoint > start {
			end = breakPoint
		}
	}

	return end
}

func findBreakPoint(runes []rune, start, end int) int {
	for i := end - 1; i > start; i-- {
		if runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\t' {
			return i + 1
		}
		// 中文标点也是好的断点
		r := runes[i]
		if r == '。' || r == '！' || r == '？' || r == '，' || r == '；' {
			return i + 1
		}
	}
	return end
}

func findOverlapStart(runes []rune, end, overlapTokens int, tokenizer Tokenizer) int {
	if overlapTokens <= 0 || end <= 0 {
		return end
	}

	// 从 end 向前找到 overlap token 对应的位置
	for i := end - 1; i >= 0; i-- {
		segment := string(runes[i:end])
		if tokenizer.Count(segment) >= overlapTokens {
			// 尝试在空白处对齐
			for j := i; j < end; j++ {
				if runes[j] == ' ' || runes[j] == '\n' {
					return j + 1
				}
			}
			return i
		}
	}
	return 0
}

// go1.21 之前没有内置 max
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// utf8RuneCount 的简单包装（保留以备用）
var _ = utf8.RuneCountInString
