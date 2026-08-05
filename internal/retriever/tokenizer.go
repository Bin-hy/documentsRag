package retriever

import (
	"strings"
	"unicode"
)

// Tokenizer BM25 分词接口
type Tokenizer interface {
	Tokenize(text string) []string
}

type simpleTokenizer struct{}

// NewSimpleTokenizer 创建简单分词器（英文按词，中文 bigram）
func NewSimpleTokenizer() Tokenizer {
	return &simpleTokenizer{}
}

func (t *simpleTokenizer) Tokenize(text string) []string {
	runes := []rune(text)
	var tokens []string
	var englishBuf []rune

	flushEnglish := func() {
		if len(englishBuf) > 0 {
			tokens = append(tokens, strings.ToLower(string(englishBuf)))
			englishBuf = nil
		}
	}

	var chineseBuf []rune

	flushChinese := func() {
		if len(chineseBuf) >= 2 {
			for i := 0; i < len(chineseBuf)-1; i++ {
				tokens = append(tokens, string(chineseBuf[i:i+2]))
			}
		} else if len(chineseBuf) == 1 {
			tokens = append(tokens, string(chineseBuf))
		}
		chineseBuf = nil
	}

	for _, r := range runes {
		if isEnglishOrDigit(r) {
			flushChinese()
			englishBuf = append(englishBuf, r)
		} else if isChinese(r) {
			flushEnglish()
			chineseBuf = append(chineseBuf, r)
		} else {
			flushEnglish()
			flushChinese()
		}
	}

	flushEnglish()
	flushChinese()

	return tokens
}

func isEnglishOrDigit(r rune) bool {
	return unicode.IsLetter(r) && r < 128 || unicode.IsDigit(r)
}

func isChinese(r rune) bool {
	return unicode.Is(unicode.Han, r)
}
