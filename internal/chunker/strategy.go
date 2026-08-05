package chunker

// Strategy 分块策略接口
type Strategy interface {
	Split(text string, config ChunkerConfig, tokenizer Tokenizer) []string
}
