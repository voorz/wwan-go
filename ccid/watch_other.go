//go:build !linux

package ccid

import (
	"context"
	"errors"
)

// ReaderEventType 标识读卡器热插拔事件的类型。
type ReaderEventType uint8

const (
	ReaderPresent ReaderEventType = iota
	ReaderAdded
	ReaderRemoved
)

// ReaderEvent 描述一个读卡器热插拔事件。
type ReaderEvent struct {
	Type   ReaderEventType
	Reader ReaderInfo
}

// Result 是 ccid 包的泛型结果类型。
type Result[T any] struct {
	Value T
	Err   error
}

var errWatchNotSupported = errors.New("WatchReaders is not supported on this platform")

// WatchReaders 在非 Linux 平台上返回不支持错误。
func WatchReaders(ctx context.Context) (<-chan Result[ReaderEvent], error) {
	return nil, errWatchNotSupported
}
