package rootaika

import (
	"fmt"
	"hash/fnv"
	"sync"
)

// configNotifier is an in-process broadcast used by long-polling config
// requests. Any config-mutating handler calls notify() to wake every blocked
// poller at once; each poller then re-reads its own effective config and
// returns only if its version actually changed. With a handful of LAN devices a
// single global broadcast is simpler and cheaper than per-device fan-out.
type configNotifier struct {
	mu sync.Mutex
	ch chan struct{}
}

func newConfigNotifier() *configNotifier {
	return &configNotifier{ch: make(chan struct{})}
}

// subscribe returns a channel closed on the next notify. Callers must subscribe
// before reading the state they intend to compare, so a change happening
// between the read and the select cannot be missed.
func (n *configNotifier) subscribe() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ch
}

func (n *configNotifier) notify() {
	n.mu.Lock()
	defer n.mu.Unlock()
	close(n.ch)
	n.ch = make(chan struct{})
}

// configVersion is a deterministic fingerprint of the fields a client acts on.
// Long-poll requests send their last-seen version; the server returns as soon
// as the recomputed version differs. Field order and the separators are fixed
// so the same effective config always yields the same value.
func configVersion(config ClientConfig) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "i=%d;u=%d;p=%d;g=%d;d=%t;l=%t;m=%q;w=%d;s=%q;",
		config.IdleThresholdSeconds,
		config.UploadIntervalSeconds,
		config.PollIntervalSeconds,
		config.MaxCountableGapSeconds,
		config.DebugMode,
		config.Locked,
		config.LockMessage,
		config.WarningSeconds,
		config.WarningSoundVersion,
	)
	for _, category := range config.Categories {
		fmt.Fprintf(h, "c=%q,%q,%q;", category.MatchType, category.Pattern, category.Category)
	}
	return fmt.Sprintf("%016x", h.Sum64())
}
