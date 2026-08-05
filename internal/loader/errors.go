package loader

import "fmt"

// ErrUnsupportedFormat 不支持的文件格式
type ErrUnsupportedFormat struct {
	Filename string
	MIMEType string
}

func (e *ErrUnsupportedFormat) Error() string {
	if e.MIMEType != "" {
		return fmt.Sprintf("不支持的文件格式: %s (MIME: %s)", e.Filename, e.MIMEType)
	}
	return fmt.Sprintf("不支持的文件格式: %s", e.Filename)
}

// ErrParseFailed 解析失败
type ErrParseFailed struct {
	Format string
	Cause  error
}

func (e *ErrParseFailed) Error() string {
	return fmt.Sprintf("解析 %s 文件失败: %v", e.Format, e.Cause)
}

func (e *ErrParseFailed) Unwrap() error {
	return e.Cause
}
