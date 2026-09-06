//go:build linux

package ccid

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	ccidUeventBufferSize  = 64 * 1024
	ccidUeventSettleDelay = 50 * time.Millisecond
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

// WatchReaders 报告初始快照和后续内核驱动的读卡器热插拔变化，直到 ctx 被取消。
// 模式仿 modem.WatchDevices：创建 NETLINK_KOBJECT_UEVENT socket，
// 过滤 subsystem=usb 且 bInterfaceClass=0x0b（CCID）的事件，
// settle 后重新调用 ListReaderInfo 做 diff。
func WatchReaders(ctx context.Context) (<-chan Result[ReaderEvent], error) {
	fd, err := openCCIDUeventSocket()
	if err != nil {
		return nil, err
	}

	initial, err := ListReaderInfo(ctx)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}

	out := make(chan Result[ReaderEvent], 16)
	go watchReaders(ctx, fd, initial, out)
	return out, nil
}

type ccidResult[T any] struct {
	Value T
	Err   error
}

// Result 是 ccid 包的泛型结果类型（与 modem.contract.Result 对齐）。
type Result[T any] ccidResult[T]

func openCCIDUeventSocket() (int, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_KOBJECT_UEVENT)
	if err != nil {
		return -1, fmt.Errorf("opening CCID uevent socket: %w", err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, ccidUeventBufferSize); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("setting CCID uevent receive buffer: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: 1}); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("binding CCID uevent socket: %w", err)
	}
	return fd, nil
}

type ccidUeventQueue struct {
	mu      sync.Mutex
	notify  chan struct{}
	rescan  bool
	stopped bool
	err     error
}

func newCCIDUeventQueue() *ccidUeventQueue {
	return &ccidUeventQueue{notify: make(chan struct{}, 1)}
}

func (q *ccidUeventQueue) push() {
	q.mu.Lock()
	q.rescan = true
	q.mu.Unlock()
	q.signal()
}

func (q *ccidUeventQueue) stop(err error) {
	q.mu.Lock()
	if !q.stopped {
		q.stopped = true
		q.err = err
	}
	q.mu.Unlock()
	q.signal()
}

func (q *ccidUeventQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *ccidUeventQueue) wait(ctx context.Context) bool {
	for {
		q.mu.Lock()
		ready := q.rescan || q.stopped
		q.mu.Unlock()
		if ready {
			return true
		}
		select {
		case <-q.notify:
		case <-ctx.Done():
			return false
		}
	}
}

func (q *ccidUeventQueue) take() (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	rescan := q.rescan
	q.rescan = false
	return rescan, q.err
}

func watchReaders(ctx context.Context, fd int, initial []ReaderInfo, out chan<- Result[ReaderEvent]) {
	defer close(out)

	current := readersByKey(initial)
	readerCtx, cancelReader := context.WithCancel(ctx)
	queue := newCCIDUeventQueue()
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		readCCIDUevents(readerCtx, fd, queue)
	}()
	defer func() {
		cancelReader()
		<-readerDone
	}()

	for _, info := range initial {
		if !sendReaderResult(ctx, out, Result[ReaderEvent]{Value: ReaderEvent{Type: ReaderPresent, Reader: info}}) {
			return
		}
	}

	for {
		if !queue.wait(ctx) || !waitCCIDUeventSettle(ctx) {
			return
		}
		rescan, queueErr := queue.take()
		if rescan {
			next, err := ListReaderInfo(ctx)
			if err != nil {
				sendReaderResult(ctx, out, Result[ReaderEvent]{Err: err})
				return
			}
			var events []ReaderEvent
			current, events = reconcileReaders(current, next)
			for _, event := range events {
				if !sendReaderResult(ctx, out, Result[ReaderEvent]{Value: event}) {
					return
				}
			}
		}
		if queueErr != nil {
			sendReaderResult(ctx, out, Result[ReaderEvent]{Err: queueErr})
			return
		}
	}
}

func readCCIDUevents(ctx context.Context, fd int, queue *ccidUeventQueue) {
	defer func() {
		_ = unix.Close(fd)
	}()
	buf := make([]byte, ccidUeventBufferSize)
	for {
		if err := ctx.Err(); err != nil {
			queue.stop(nil)
			return
		}
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pollFDs, 500)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			queue.stop(fmt.Errorf("waiting for CCID uevent: %w", err))
			return
		}
		if n == 0 {
			continue
		}
		if pollFDs[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			queue.stop(fmt.Errorf("CCID uevent socket stopped: revents=0x%X", uint16(pollFDs[0].Revents)))
			return
		}
		length, _, err := unix.Recvfrom(fd, buf, 0)
		if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			queue.stop(fmt.Errorf("reading CCID uevent: %w", err))
			return
		}
		if isCCIDUevent(buf[:length]) {
			queue.push()
		}
	}
}

// isCCIDUevent 检查 uevent 是否与 CCID 读卡器相关。
// CCID 读卡器的 USB 接口类为 0x0b，uevent 中 INTERFACE=11/... 或 SUBSYSTEM=usb。
func isCCIDUevent(data []byte) bool {
	s := string(data)
	if !strings.Contains(s, "ACTION=add") && !strings.Contains(s, "ACTION=remove") {
		return false
	}
	if strings.Contains(s, "INTERFACE=11/") {
		return true
	}
	if strings.Contains(s, "SUBSYSTEM=usb") && strings.Contains(s, "bInterfaceClass=0b") {
		return true
	}
	return false
}

func waitCCIDUeventSettle(ctx context.Context) bool {
	timer := time.NewTimer(ccidUeventSettleDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func readersByKey(readers []ReaderInfo) map[string]ReaderInfo {
	result := make(map[string]ReaderInfo, len(readers))
	for _, r := range readers {
		key := readerKey(r)
		result[key] = r
	}
	return result
}

func readerKey(r ReaderInfo) string {
	if r.USBPath != "" {
		return r.USBPath
	}
	return r.Name
}

func reconcileReaders(current map[string]ReaderInfo, next []ReaderInfo) (map[string]ReaderInfo, []ReaderEvent) {
	nextByKey := readersByKey(next)
	keys := make(map[string]struct{}, len(current)+len(next))
	for k := range current {
		keys[k] = struct{}{}
	}
	for k := range nextByKey {
		keys[k] = struct{}{}
	}
	sortedKeys := make([]string, 0, len(keys))
	for k := range keys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	var events []ReaderEvent
	for _, key := range sortedKeys {
		before, existed := current[key]
		after, exists := nextByKey[key]
		switch {
		case !existed && exists:
			events = append(events, ReaderEvent{Type: ReaderAdded, Reader: after})
		case existed && !exists:
			events = append(events, ReaderEvent{Type: ReaderRemoved, Reader: before})
		case existed && exists && !sameReader(before, after):
			events = append(events,
				ReaderEvent{Type: ReaderRemoved, Reader: before},
				ReaderEvent{Type: ReaderAdded, Reader: after},
			)
		}
	}
	return nextByKey, slices.Clip(events)
}

func sameReader(a, b ReaderInfo) bool {
	return a.Name == b.Name && a.USBPath == b.USBPath && a.VendorID == b.VendorID && a.ProductID == b.ProductID
}

func sendReaderResult(ctx context.Context, out chan<- Result[ReaderEvent], result Result[ReaderEvent]) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case out <- result:
		return true
	case <-ctx.Done():
		return false
	}
}
