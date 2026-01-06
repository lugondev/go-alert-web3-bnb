package telegram

import (
	"context"
	"fmt"
	"sync"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
)

type MultiNotifier struct {
	notifiers []*Notifier
	log       logger.Logger
}

func NewMultiNotifier(configs []Config, log logger.Logger) *MultiNotifier {
	notifiers := make([]*Notifier, 0, len(configs))

	for _, cfg := range configs {
		if cfg.BotToken != "" && cfg.ChatID != "" {
			notifiers = append(notifiers, NewNotifier(cfg, log))
		}
	}

	return &MultiNotifier{
		notifiers: notifiers,
		log:       log.With(logger.F("component", "multi_telegram")),
	}
}

func (m *MultiNotifier) SendMarkdown(ctx context.Context, text string) error {
	if len(m.notifiers) == 0 {
		return fmt.Errorf("no notifiers configured")
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(m.notifiers))

	for i, notifier := range m.notifiers {
		wg.Add(1)
		go func(idx int, n *Notifier) {
			defer wg.Done()

			if err := n.SendMarkdown(ctx, text); err != nil {
				m.log.Error("failed to send via notifier",
					logger.F("notifier_index", idx),
					logger.F("error", err),
				)
				errChan <- err
			}
		}(i, notifier)
	}

	wg.Wait()
	close(errChan)

	var firstErr error
	for err := range errChan {
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (m *MultiNotifier) IsEnabled() bool {
	for _, n := range m.notifiers {
		if n.IsEnabled() {
			return true
		}
	}
	return false
}

func (m *MultiNotifier) Count() int {
	return len(m.notifiers)
}
