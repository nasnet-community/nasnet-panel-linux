package contract

// CoreTrafficRef identifies a subscription's core tunnel on a node.
type CoreTrafficRef struct {
	ID             uint
	SubscriptionID uint
	Protocol       string
}

// CoreUsageDelta carries the bytes attributed to one core tunnel.
type CoreUsageDelta struct {
	CoreUserID uint
	Bytes      int64
}
