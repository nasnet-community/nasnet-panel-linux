package notification

import (
	"context"
	"strings"
	"sync"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/events"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

const subscriberID = "notification-dispatcher"

// Dispatcher subscribes to the EventBus and routes notifications to channels.
type Dispatcher struct {
	eventBus *events.EventBus
	channels []Channel
	settings SettingProvider
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewDispatcher creates a new notification dispatcher.
func NewDispatcher(eventBus *events.EventBus, settings SettingProvider, channels ...Channel) *Dispatcher {
	return &Dispatcher{
		eventBus: eventBus,
		channels: channels,
		settings: settings,
	}
}

// Start begins listening for events on the EventBus.
func (d *Dispatcher) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)
	sub := d.eventBus.Subscribe(subscriberID)

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.listen(ctx, sub)
	}()

	logger.GetLogger().Info("Notification Dispatcher started")
}

// Stop unsubscribes from the EventBus and waits for the listener to exit.
func (d *Dispatcher) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.eventBus.Unsubscribe(subscriberID)
	d.wg.Wait()
	logger.GetLogger().Info("Notification Dispatcher stopped")
}

func (d *Dispatcher) listen(ctx context.Context, sub events.Subscriber) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-sub:
			if !ok {
				return // channel closed
			}
			d.handleEvent(ctx, event)
		}
	}
}

func (d *Dispatcher) handleEvent(ctx context.Context, event events.Event) {
	log := logger.GetLogger()
	// Skip noisy events that should never produce notifications
	if event.Type == events.EventNodeStatsUpdated {
		return
	}

	// Look up template
	tmplFn := getTemplate(event.Type)
	if tmplFn == nil {
		return
	}

	msg := tmplFn(event)
	if msg == nil {
		return
	}

	// Send to each channel concurrently
	var wg sync.WaitGroup
	for _, ch := range d.channels {
		ch := ch
		// Check if this channel+event combination is enabled
		settingKey := "notification_" + ch.Name() + "_" + eventTypeToSettingKey(string(event.Type))
		enabled := d.isEnabled(ctx, settingKey, ch.Name(), string(event.Type))
		if !enabled {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ch.Send(ctx, msg); err != nil {
				log.WithError(err).WithField("channel", ch.Name()).WithField("event", string(event.Type)).Warn("[Dispatcher] Failed to send notification")
			}
		}()
	}
	wg.Wait()
}

// isEnabled checks whether a notification channel+event is enabled via settings.
func (d *Dispatcher) isEnabled(ctx context.Context, settingKey, channelName, eventType string) bool {
	val, err := d.settings.GetByKey(ctx, settingKey)
	if err != nil {
		// Setting not found — use defaults
		return d.defaultEnabled(channelName, eventType)
	}
	return strings.EqualFold(val, "true")
}

// defaultEnabled returns the default state for a channel+event when no setting exists.
func (d *Dispatcher) defaultEnabled(channelName, eventType string) bool {
	if channelName == "telegram" {
		// Default telegram ON for critical events
		switch events.EventType(eventType) {
		case events.EventNodeOnline, events.EventNodeOffline,
			events.EventSubscriptionCreated, events.EventSystemAlert,
			events.EventXrayDown, events.EventXrayUp, events.EventXrayCrashLoop,
			events.EventXrayRecoveryCommand, events.EventXrayRecoveryExhausted:
			return true
		}
	}
	return false
}
