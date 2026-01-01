package accesslog

import (
	"bufio"
	"io"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Collector tails an xray access log file and feeds parsed entries into a Store.
type Collector struct {
	filePath   string
	store      *Store
	aggregator *Aggregator
	parser     *Parser
	firstOpen  bool // true until the first successful file open

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewCollector creates a new access log collector.
func NewCollector(filePath string, store *Store, aggregator *Aggregator) *Collector {
	return &Collector{
		filePath:   filePath,
		store:      store,
		aggregator: aggregator,
		parser:     NewParser(),
		firstOpen:  true,
		stopCh:     make(chan struct{}),
	}
}

// Aggregator returns the underlying aggregator for querying buffered summaries.
func (c *Collector) Aggregator() *Aggregator {
	return c.aggregator
}

// Start begins tailing the access log file in a background goroutine.
func (c *Collector) Start() {
	c.wg.Add(1)
	go c.loop()
	logrus.WithField("file", c.filePath).Info("[accesslog] Collector started")
}

// Stop halts the collector, persists aggregator state, and waits for it to finish.
// Idempotent — Server.Stop can be reached twice during SelfUpdate (SIGTERM
// handler plus restartSelf fallback), and a double close(c.stopCh) would panic.
func (c *Collector) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.wg.Wait()
		if c.aggregator != nil {
			c.aggregator.Persist()
		}
		logrus.Info("[accesslog] Collector stopped")
	})
}

// Store returns the underlying store for querying.
func (c *Collector) Store() *Store {
	return c.store
}

func (c *Collector) loop() {
	defer c.wg.Done()

	for {
		err := c.tailFile()
		if err != nil {
			logrus.WithError(err).Debug("[accesslog] Tail interrupted, will retry")
		}

		// Wait before retrying (file may not exist yet or was rotated)
		select {
		case <-c.stopCh:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// tailFile opens the file, seeks to end, and reads new lines until the file
// is rotated/truncated or the collector is stopped.
func (c *Collector) tailFile() error {
	f, err := os.Open(c.filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Get initial file info for rotation detection
	initInfo, err := f.Stat()
	if err != nil {
		return err
	}

	// On first open, read existing content to populate the store immediately.
	// On subsequent opens (after rotation), seek to end for new entries only.
	if c.firstOpen {
		c.firstOpen = false
	} else {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
	}

	reader := bufio.NewReader(f)
	parsed, skipped := 0, 0
	totalParsed, totalSkipped := 0, 0
	lastProgressLog := time.Now()

	for {
		select {
		case <-c.stopCh:
			return nil
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return err
			}

			// Accumulate counts
			totalParsed += parsed
			totalSkipped += skipped
			parsed, skipped = 0, 0

			// Log progress at most every 60 seconds
			if totalParsed > 0 || totalSkipped > 0 {
				if time.Since(lastProgressLog) >= 60*time.Second {
					logrus.WithFields(logrus.Fields{
						"parsed":  totalParsed,
						"skipped": totalSkipped,
						"total":   c.store.Len(),
					}).Info("[accesslog] Tail progress")
					totalParsed, totalSkipped = 0, 0
					lastProgressLog = time.Now()
				}
			}

			// EOF — check for rotation/truncation
			if rotated, _ := c.isRotated(f, initInfo); rotated {
				return nil // re-open in outer loop
			}

			// No new data, poll
			select {
			case <-c.stopCh:
				return nil
			case <-time.After(200 * time.Millisecond):
			}
			continue
		}

		if entry := c.parser.Parse(line); entry != nil {
			c.store.Add(*entry)
			if c.aggregator != nil {
				c.aggregator.Record(*entry)
			}
			parsed++
		} else {
			skipped++
		}
	}
}

// isRotated checks if the file has been truncated or replaced.
func (c *Collector) isRotated(f *os.File, initInfo os.FileInfo) (bool, error) {
	// Check if file on disk has changed (different inode or smaller size)
	diskInfo, err := os.Stat(c.filePath)
	if err != nil {
		return true, err // file gone → treat as rotated
	}

	currentPos, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return false, err
	}

	// File was truncated if current size is smaller than our read position
	if diskInfo.Size() < currentPos {
		return true, nil
	}

	// Check if inode changed (file was replaced)
	if !os.SameFile(initInfo, diskInfo) {
		return true, nil
	}

	return false, nil
}
