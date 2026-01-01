package tool

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func GenSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func GenPassword(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, n)
	for i := range result {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[idx.Int64()]
	}
	return string(result)
}

func GenBcrypt(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func DetectIP(offline bool) string {
	if offline {
		return detectLocalIP()
	}
	for _, url := range []string{"https://api.ipify.org", "https://ifconfig.me"} {
		client := wizardHTTPClient(nil)
		client.Timeout = 5 * time.Second
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		buf := make([]byte, 64)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		ip := strings.TrimSpace(string(buf[:n]))
		if ip != "" {
			return ip
		}
	}
	return detectLocalIP()
}

func detectLocalIP() string {
	out, err := exec.Command("hostname", "-I").Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}

func RandomPort() string {
	for range 20 {
		n, _ := rand.Int(rand.Reader, big.NewInt(50001))
		port := int(n.Int64()) + 10000
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
			return fmt.Sprintf("%d", port)
		}
	}
	return "9761"
}

func MaskSecret(val string, show int) string {
	if len(val) <= show {
		return "****"
	}
	return val[:show] + strings.Repeat("*", len(val)-show)
}

func ReadEnvValue(key, filePath string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if val, ok := strings.CutPrefix(line, key+"="); ok {
			val = strings.Trim(val, "'\"")
			return val
		}
	}
	return ""
}

// bytesHuman converts bytes to a human-readable string.
func bytesHuman(b int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
