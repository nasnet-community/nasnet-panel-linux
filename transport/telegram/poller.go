package telegram

import (
	"encoding/json"
	"strconv"
	"time"

	"gopkg.in/telebot.v3"
)

// Consecutive-failure backoff for long polling. Stock telebot's LongPoller
// retries a refused getUpdates as fast as the kernel rejects connections and
// only reports it under Verbose; this one backs off and logs through OnError.
const (
	pollRetryBase = time.Second
	pollRetryMax  = 15 * time.Second
)

type backoffPoller struct {
	timeout      time.Duration
	lastUpdateID int
}

func newBackoffPoller(timeout time.Duration) *backoffPoller {
	return &backoffPoller{timeout: timeout}
}

func (p *backoffPoller) Poll(b *telebot.Bot, dest chan telebot.Update, stop chan struct{}) {
	var retryDelay time.Duration
	for {
		select {
		case <-stop:
			return
		default:
		}

		updates, err := p.getUpdates(b)
		if err != nil {
			b.OnError(err, nil)
			if retryDelay == 0 {
				retryDelay = pollRetryBase
			} else if retryDelay < pollRetryMax {
				retryDelay = min(retryDelay*2, pollRetryMax)
			}
			select {
			case <-stop:
				return
			case <-time.After(retryDelay):
			}
			continue
		}
		retryDelay = 0

		for _, u := range updates {
			p.lastUpdateID = u.ID
			dest <- u
		}
	}
}

func (p *backoffPoller) getUpdates(b *telebot.Bot) ([]telebot.Update, error) {
	params := map[string]string{
		"offset":  strconv.Itoa(p.lastUpdateID + 1),
		"timeout": strconv.Itoa(int(p.timeout / time.Second)),
	}
	data, err := b.Raw("getUpdates", params)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result []telebot.Update `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}
