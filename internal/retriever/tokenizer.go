package retriever

import (
	"unicode"
)

// Tokenizer BM25 分词接口
type Tokenizer interface {
	Tokenize(text string) []string
}

// --- 可选项 ---

// tokenizerConfig 分词器内部配置
type tokenizerConfig struct {
	cjkUnigram bool                // 是否额外输出中文 unigram（提升单字查询召回率）
	stopWords  map[string]struct{} // 英文停用词集合（小写后匹配）
}

// TokenizerOption 分词器可选配置项
type TokenizerOption func(*tokenizerConfig)

// WithCJKUnigram 让中文在 bigram 之外额外输出 unigram。
// 默认只输出 bigram（"向量数据库" -> 向量/量数/数据/据库），
// 单字查询（如"库"）将无法命中；开启后可提升召回，但索引体积略增。
func WithCJKUnigram() TokenizerOption {
	return func(c *tokenizerConfig) { c.cjkUnigram = true }
}

// WithEnglishStopWords 指定英文停用词集合（不区分大小写，匹配前会小写化）。
// 命中停用词的英文 token 将被丢弃。
func WithEnglishStopWords(words ...string) TokenizerOption {
	return func(c *tokenizerConfig) {
		if c.stopWords == nil {
			c.stopWords = make(map[string]struct{}, len(words))
		}
		for _, w := range words {
			// 停用词统一转小写存储
			buf := []rune(w)
			for i, r := range buf {
				if r >= 'A' && r <= 'Z' {
					buf[i] = r + 32
				}
			}
			c.stopWords[string(buf)] = struct{}{}
		}
	}
}

// WithDefaultEnglishStopWords 使用内置的常见英文停用词表。
func WithDefaultEnglishStopWords() TokenizerOption {
	return WithEnglishStopWords(
		"a", "an", "the", "is", "are", "was", "were", "be", "been",
		"of", "to", "in", "on", "at", "by", "for", "with", "as",
		"and", "or", "not", "but", "if", "then", "else", "so",
		"it", "its", "this", "that", "these", "those",
		"i", "you", "he", "she", "we", "they", "me", "him", "her", "us", "them",
		"do", "does", "did", "have", "has", "had", "will", "would", "can", "could",
	)
}

// --- 字符分类 ---

// charClass 字符类别
type charClass uint8

const (
	classOther charClass = iota // 标点、空白等边界字符
	classAlnum                  // ASCII 字母或数字
	classHan                    // 汉字（含 CJK 统一表意文字全部扩展区）
)

// classify 对单个 rune 分类。
// 常用汉字集中在 U+4E00 ~ U+9FFF 基本区，先做范围快速判断，
// 避免每次都走 unicode.Han 全局脚本表查询。
func classify(r rune) charClass {
	switch {
	case r < 128:
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return classAlnum
		}
		return classOther
	case r >= 0x4E00 && r <= 0x9FFF:
		return classHan
	default:
		if unicode.Is(unicode.Han, r) {
			return classHan
		}
		return classOther
	}
}

// --- 实现 ---

// simpleTokenizer 简单分词器：英文/数字按词聚合（小写化），中文按 bigram 切分。
// 无内部可变共享状态，并发安全。
type simpleTokenizer struct {
	cfg tokenizerConfig
}

// NewSimpleTokenizer 创建简单分词器（英文按词，中文 bigram）。
// 默认行为与旧版完全一致；可通过 TokenizerOption 增强：
//
//	retriever.NewSimpleTokenizer(retriever.WithCJKUnigram(), retriever.WithDefaultEnglishStopWords())
func NewSimpleTokenizer(opts ...TokenizerOption) Tokenizer {
	cfg := tokenizerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &simpleTokenizer{cfg: cfg}
}

func (t *simpleTokenizer) Tokenize(text string) []string {
	if text == "" {
		return nil
	}

	runes := []rune(text)
	// 预估容量：纯中文 bigram 约为 rune 数，纯英文远少于 rune 数，取 rune 数的一半作为折中
	tokens := make([]string, 0, len(runes)/2+2)

	// 复用缓冲区，避免每次 flush 重新分配
	var enBuf []rune
	var zhBuf []rune

	// flushEnglish 输出英文 token（ASCII 位运算小写化，并做停用词过滤）
	flushEnglish := func() {
		if len(enBuf) == 0 {
			return
		}
		for i, r := range enBuf {
			if r >= 'A' && r <= 'Z' {
				enBuf[i] = r + 32 // 'A'~'Z' 区间 +32 即转小写
			}
		}
		word := string(enBuf)
		if _, stopped := t.cfg.stopWords[word]; !stopped {
			tokens = append(tokens, word)
		}
		enBuf = enBuf[:0]
	}

	// flushChinese 输出中文 token：bigram 为主，可选 unigram
	flushChinese := func() {
		n := len(zhBuf)
		switch {
		case n == 0:
			return
		case n == 1:
			tokens = append(tokens, string(zhBuf[0]))
		default:
			if t.cfg.cjkUnigram {
				for _, r := range zhBuf {
					tokens = append(tokens, string(r))
				}
			}
			for i := 0; i+1 < n; i++ {
				tokens = append(tokens, string(zhBuf[i:i+2]))
			}
		}
		zhBuf = zhBuf[:0]
	}

	for _, r := range runes {
		switch classify(r) {
		case classAlnum:
			flushChinese()
			enBuf = append(enBuf, r)
		case classHan:
			flushEnglish()
			zhBuf = append(zhBuf, r)
		default:
			flushEnglish()
			flushChinese()
		}
	}
	flushEnglish()
	flushChinese()

	return tokens
}
