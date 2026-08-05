package chunker

import (
	"strings"
	"unicode"
)

// Tokenizer token 计数接口
type Tokenizer interface {
	Count(text string) int
}

// DefaultTokenizer 简单估算器
type DefaultTokenizer struct{}

func NewDefaultTokenizer() Tokenizer {
	return &DefaultTokenizer{}
}

func (t *DefaultTokenizer) Count(text string) int {
	if text == "" {
		return 0
	}

	count := 0
	var wordBuf strings.Builder

	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			// 中文字符计 2 token
			if wordBuf.Len() > 0 {
				count++
				wordBuf.Reset()
			}
			count += 2
		} else if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			if wordBuf.Len() > 0 {
				count++
				wordBuf.Reset()
			}
			count++
		} else if unicode.IsSpace(r) {
			if wordBuf.Len() > 0 {
				count++
				wordBuf.Reset()
			}
		} else {
			wordBuf.WriteRune(r)
		}
	}

	if wordBuf.Len() > 0 {
		count++
	}

	return count
}
