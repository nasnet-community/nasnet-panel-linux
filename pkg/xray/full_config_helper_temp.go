package xray

// getLogLevel returns the log level or default
func (b *FullConfigBuilder) getLogLevel() string {
	if b.node.LogLevel != "" {
		return b.node.LogLevel
	}
	return "warning"
}
