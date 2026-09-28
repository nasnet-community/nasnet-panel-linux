package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/repository"
	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/auth"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/logger"
)

// validOSArchPattern validates OS and arch query parameters to prevent path traversal.
var validOSArchPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func isValidOSArch(v string) bool {
	return len(v) <= 20 && validOSArchPattern.MatchString(v)
}

// releaseTagPattern is a clean release tag; anything else (dev, dirty,
// describe output) has no GitHub release to fetch the agent from.
var releaseTagPattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

const agentReleaseURL = "https://github.com/nasnet-community/nasnet-panel-linux/releases/download"

// deployArches are the agent builds the panel ships.
var deployArches = []string{"amd64", "arm64"}

// XrayChecksums is the slice of the xray binary cache the installers need.
type XrayChecksums interface {
	EnsureCached(version, arch string) error
	GetChecksum(version, arch string) (string, error)
}

// DeployHandler handles agent deployment endpoints
type DeployHandler struct {
	nodeRepo      repository.NodeRepository
	nodeUC        usecase.NodeUsecase
	tokenManager  *auth.TokenManager
	certUC        usecase.CertificateUsecase
	usedTokenRepo repository.UsedTokenRepository
	settingUC     settingDomain.SettingUsecase
	xray          XrayChecksums
	baseURL       string
	version       string
	caInitMu      sync.Mutex // Protects concurrent CA initialization
}

// NewDeployHandler creates a new deploy handler
func NewDeployHandler(nodeRepo repository.NodeRepository, nodeUC usecase.NodeUsecase, tokenManager *auth.TokenManager, certUC usecase.CertificateUsecase, usedTokenRepo repository.UsedTokenRepository, baseURL string, settingUC settingDomain.SettingUsecase, xray XrayChecksums, version string) *DeployHandler {
	return &DeployHandler{
		nodeRepo:      nodeRepo,
		nodeUC:        nodeUC,
		tokenManager:  tokenManager,
		certUC:        certUC,
		usedTokenRepo: usedTokenRepo,
		settingUC:     settingUC,
		xray:          xray,
		baseURL:       baseURL,
		version:       version,
	}
}

// RegisterAdminRoutes registers deployment routes requiring admin access
func (h *DeployHandler) RegisterAdminRoutes(g *gin.RouterGroup) {
	deploy := g.Group("/deploy")
	{
		deploy.GET("/command/:node_id", h.GetInstallCommand)
		deploy.GET("/paste/:node_id", h.GetPasteScript)
	}
}

// RegisterPublicRoutes registers deployment routes that use token authentication
func (h *DeployHandler) RegisterPublicRoutes(g *gin.RouterGroup) {
	deploy := g.Group("/deploy")
	{
		deploy.GET("/script/:node_id", h.GetInstallScript)
		deploy.GET("/binary", h.GetAgentBinary)
		deploy.GET("/binary/checksum", h.GetAgentChecksum)
	}
}

func (h *DeployHandler) setting(c *gin.Context, key string) string {
	if h.settingUC == nil {
		return ""
	}
	v, _ := h.settingUC.GetByKey(c.Request.Context(), key)
	return strings.TrimSpace(v)
}

// pullBaseURL is the https address remote nodes fetch the installer from,
// panel base path included; "" when pull install isn't safe to offer.
// Never the LAN app_base_url: over plain http the token, the binary and the
// node key would cross the internet in clear.
func (h *DeployHandler) pullBaseURL(c *gin.Context) string {
	u, err := url.Parse(h.setting(c, "agent_hub_url"))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	base := strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
	if strings.Trim(u.Path, "/") == "" {
		base += strings.TrimRight(h.setting(c, "panel_base_path"), "/")
	}
	return base
}

// hubDialAddr is host:port a reverse-mode agent dials; "" without an
// agent_hub_url (the base URL is a LAN address on routers).
func (h *DeployHandler) hubDialAddr(c *gin.Context) string {
	raw := h.setting(c, "agent_hub_url")
	if raw == "" {
		return ""
	}
	host := raw
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		host = u.Hostname()
	} else if hh, _, err := net.SplitHostPort(raw); err == nil {
		host = hh
	}
	port := 9091
	if p, err := strconv.Atoi(h.setting(c, "hub_port")); err == nil && p > 0 && p <= 65535 {
		port = p
	}
	if strings.Contains(host, ":") {
		return fmt.Sprintf("[%s]:%d", strings.Trim(host, "[]"), port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func (h *DeployHandler) xrayVersion(c *gin.Context) string {
	if v := h.setting(c, "xray_default_version"); v != "" {
		return v
	}
	return "26.7.28"
}

// ensureCA creates the hub CA on first deploy; agents need it for mTLS.
func (h *DeployHandler) ensureCA(c *gin.Context) error {
	h.caInitMu.Lock()
	defer h.caInitMu.Unlock()
	if h.certUC.HasCA(c.Request.Context()) {
		return nil
	}
	logger.GetLogger().Info("[Deploy] CA not initialized, creating it with defaults")
	_, err := h.certUC.InitializeCA(c.Request.Context(), nil)
	return err
}

// deployableNode loads the node an installer is for, refusing the built-in one.
func (h *DeployHandler) deployableNode(c *gin.Context) (*domain.Node, bool) {
	nodeID, err := strconv.ParseUint(c.Param("node_id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid node ID"})
		return nil, false
	}
	node, err := h.nodeRepo.GetNode(c.Request.Context(), uint(nodeID))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Node not found"})
		return nil, false
	}
	if node.IsLocal() {
		c.JSON(http.StatusConflict, gin.H{"error": "The built-in node runs inside the panel and needs no agent", "code": "NODE_LOCAL"})
		return nil, false
	}
	return node, true
}

func bearerToken(c *gin.Context) string {
	if hdr := c.GetHeader("Authorization"); len(hdr) >= 7 && hdr[:7] == "Bearer " {
		return hdr[7:]
	}
	return c.Query("token")
}

func agentPort(n *domain.Node) int {
	if n.AgentPort == 0 {
		return 9090
	}
	return n.AgentPort
}

func connectMode(n *domain.Node) string {
	if n.ConnectMode == "" {
		return domain.ConnectModeDirect
	}
	return n.ConnectMode
}

// GetInstallCommand says how to install a node's agent: a one-time pull
// command when the panel has an https agent hub address, else the
// self-contained paste script.
func (h *DeployHandler) GetInstallCommand(c *gin.Context) {
	node, ok := h.deployableNode(c)
	if !ok {
		return
	}
	if err := h.ensureCA(c); err != nil {
		logger.GetLogger().WithError(err).Error("[GetInstallCommand] Failed to auto-initialize CA")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize Certificate Authority", "details": err.Error()})
		return
	}

	data := gin.H{
		"node_id":      node.ID,
		"node_name":    node.Name,
		"node_ip":      node.IP,
		"port":         agentPort(node),
		"connect_mode": connectMode(node),
		"paste_url":    fmt.Sprintf("/api/v1/deploy/paste/%d", node.ID),
	}

	base := h.pullBaseURL(c)
	if base == "" {
		data["mode"] = "paste"
		data["command"] = ""
		data["reason"] = "Set an https Agent Hub Address in settings to install with one command; until then, run the installer script on the node."
		if connectMode(node) == domain.ConnectModeReverse && h.hubDialAddr(c) == "" {
			data["reason"] = "Reverse-mode nodes dial the panel. Set the Agent Hub Address in settings first."
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
		return
	}

	token, err := h.tokenManager.GenerateDeploymentToken(node.ID, time.Hour)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate deployment token"})
		return
	}
	scriptURL := fmt.Sprintf("%s/api/v1/deploy/script/%d", base, node.ID)
	data["mode"] = "pull"
	data["command"] = fmt.Sprintf(`curl -fsSL -H "Authorization: Bearer %s" "%s" | bash`, token, scriptURL)
	data["script_url"] = scriptURL
	data["expires_at"] = time.Now().Add(time.Hour).Unix()
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

// GetInstallScript serves the pull installer. The token is one-time.
func (h *DeployHandler) GetInstallScript(c *gin.Context) {
	token := bearerToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing deployment token"})
		return
	}
	claims, err := h.tokenManager.ValidateDeploymentToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired deployment token"})
		return
	}
	node, ok := h.deployableNode(c)
	if !ok {
		return
	}
	if claims.NodeID != node.ID {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired deployment token"})
		return
	}
	if !h.markTokenUsed(c, claims, "") {
		return
	}

	base := h.pullBaseURL(c)
	if base == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "Pull install needs an https Agent Hub Address"})
		return
	}
	script, err := renderInstallScript(installScript{
		NodeID:      node.ID,
		AgentPort:   agentPort(node),
		ConnectMode: connectMode(node),
		NodeUUID:    node.UUID,
		HubURL:      h.hubDialAddr(c),
		XrayVersion: h.xrayVersion(c),
		MasterURL:   base,
		Token:       token,
	})
	if err != nil {
		logger.GetLogger().WithError(err).Error("[GetInstallScript] Failed to render the installer")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to render the installer"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "text/x-shellscript")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=install-agent-%d.sh", node.ID))
	c.String(http.StatusOK, script)
}

// markTokenUsed records the token's jti (plus an optional purpose suffix) so
// it can't be replayed. It writes the error response when it returns false.
func (h *DeployHandler) markTokenUsed(c *gin.Context, claims *auth.DeploymentClaims, purpose string) bool {
	return MarkDeploymentTokenUsed(c, h.usedTokenRepo, claims, purpose)
}

// MarkDeploymentTokenUsed enforces one-time use through the unique jti
// column. Shared with the certificate bundle route.
func MarkDeploymentTokenUsed(c *gin.Context, repo repository.UsedTokenRepository, claims *auth.DeploymentClaims, purpose string) bool {
	if repo == nil || claims.ID == "" {
		return true
	}
	jti := claims.ID
	if purpose != "" {
		jti += ":" + purpose
	}
	if err := repo.MarkUsed(c.Request.Context(), jti, claims.NodeID, claims.ExpiresAt.Time); err != nil {
		if isDuplicateKeyError(err) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Deployment token has already been used"})
			return false
		}
		logger.GetLogger().WithError(err).Warn("[Deploy] Failed to mark token as used")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to verify token"})
		return false
	}
	return true
}

// GetPasteScript renders a self-contained installer for nodes that can't
// reach the panel: CA, a fresh node certificate and key, and the agent
// config inline; the agent and xray come from their GitHub releases,
// checked against checksums embedded here. Nothing in it calls back.
func (h *DeployHandler) GetPasteScript(c *gin.Context) {
	node, ok := h.deployableNode(c)
	if !ok {
		return
	}
	hubURL := ""
	if connectMode(node) == domain.ConnectModeReverse {
		if hubURL = h.hubDialAddr(c); hubURL == "" {
			c.JSON(http.StatusConflict, gin.H{"error": "Reverse-mode nodes dial the panel. Set the Agent Hub Address in settings first."})
			return
		}
	}
	if err := h.ensureCA(c); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize Certificate Authority", "details": err.Error()})
		return
	}

	agentSHA := map[string]string{}
	for _, arch := range deployArches {
		if sum, err := fileSHA256(agentBinaryPath("linux", arch)); err == nil {
			agentSHA[arch] = sum
		}
	}
	if len(agentSHA) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "This panel has no agent builds (bin/agent). Update the panel or run make build-agent."})
		return
	}

	xrayVersion := h.xrayVersion(c)
	xraySHA := map[string]string{}
	if h.xray != nil {
		for _, arch := range deployArches {
			// Best effort: without a cached copy the script checks XTLS's digest.
			if err := h.xray.EnsureCached(xrayVersion, arch); err == nil {
				if sum, err := h.xray.GetChecksum(xrayVersion, arch); err == nil {
					xraySHA[arch] = sum
				}
			}
		}
	}

	bundle, err := h.certUC.GetCertificateBundle(c.Request.Context(), node.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to issue the node certificate", "details": err.Error()})
		return
	}

	script, err := renderInstallScript(installScript{
		NodeID:       node.ID,
		AgentPort:    agentPort(node),
		ConnectMode:  connectMode(node),
		NodeUUID:     node.UUID,
		HubURL:       hubURL,
		XrayVersion:  xrayVersion,
		Paste:        true,
		CACert:       strings.TrimSpace(string(bundle.CACert)),
		AgentCert:    strings.TrimSpace(string(bundle.AgentCert)),
		AgentKey:     strings.TrimSpace(string(bundle.AgentKey)),
		AgentURLBase: h.agentReleaseBase(),
		AgentSHA:     agentSHA,
		XraySHA:      xraySHA,
	})
	if err != nil {
		logger.GetLogger().WithError(err).Error("[GetPasteScript] Failed to render the installer")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to render the installer"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "text/x-shellscript")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=install-agent-%d.sh", node.ID))
	c.String(http.StatusOK, script)
}

// agentReleaseBase is where the paste installer downloads the agent; ""
// when this panel isn't a release build (the script then needs AGENT_FILE).
func (h *DeployHandler) agentReleaseBase() string {
	if !releaseTagPattern.MatchString(h.version) {
		return ""
	}
	return agentReleaseURL + "/" + h.version
}

func agentBinaryPath(osName, arch string) string {
	return filepath.Join("bin", "agent", fmt.Sprintf("nasnet-agent-%s-%s", osName, arch))
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// agentBinaryFile resolves and checks the requested build, writing the
// error response when it returns "".
func (h *DeployHandler) agentBinaryFile(c *gin.Context) string {
	requestedOS, requestedArch := c.Query("os"), c.Query("arch")
	if requestedOS == "" {
		requestedOS = runtime.GOOS
	}
	if requestedArch == "" {
		requestedArch = runtime.GOARCH
	}
	if !isValidOSArch(requestedOS) || !isValidOSArch(requestedArch) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid os/arch parameter"})
		return ""
	}
	token := bearerToken(c)
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Missing or invalid authorization token"})
		return ""
	}
	if _, err := h.tokenManager.ValidateDeploymentToken(token); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid deployment token"})
		return ""
	}
	absPath, err := filepath.Abs(agentBinaryPath(requestedOS, requestedArch))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return ""
	}
	binDir, _ := filepath.Abs(filepath.Join("bin", "agent"))
	if !strings.HasPrefix(absPath, binDir+string(os.PathSeparator)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid os/arch parameter"})
		return ""
	}
	if _, err := os.Stat(absPath); err != nil {
		logger.GetLogger().WithField("path", absPath).Warn("[Deploy] Agent binary not found")
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("Binary not found for %s/%s", requestedOS, requestedArch),
			"help":  "Update the panel (releases ship bin/agent) or run 'make build-agent' on the panel host.",
		})
		return ""
	}
	return absPath
}

// GetAgentBinary returns the agent binary for the requested OS/arch
func (h *DeployHandler) GetAgentBinary(c *gin.Context) {
	path := h.agentBinaryFile(c)
	if path == "" {
		return
	}
	if sum, err := fileSHA256(path); err == nil {
		c.Header("X-Binary-SHA256", sum)
	}
	c.File(path)
}

// GetAgentChecksum returns the SHA256 checksum of the agent binary
func (h *DeployHandler) GetAgentChecksum(c *gin.Context) {
	path := h.agentBinaryFile(c)
	if path == "" {
		return
	}
	sum, err := fileSHA256(path)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read binary"})
		return
	}
	c.String(http.StatusOK, sum)
}

func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	var target interface{ SQLState() string }
	if errors.As(err, &target) && target.SQLState() == "23505" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "unique_violation") || strings.Contains(msg, "duplicated key")
}

// ---- installer script ----

type installScript struct {
	NodeID      uint
	AgentPort   int
	ConnectMode string
	NodeUUID    string
	HubURL      string
	XrayVersion string

	// Pull install
	MasterURL string
	Token     string

	// Paste install
	Paste        bool
	CACert       string
	AgentCert    string
	AgentKey     string
	AgentURLBase string
	AgentSHA     map[string]string
	XraySHA      map[string]string
}

// shellQuote wraps s in single quotes for bash.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var installTemplate = template.Must(template.New("install").Funcs(template.FuncMap{"q": shellQuote}).Parse(installScriptTemplate))

func renderInstallScript(p installScript) (string, error) {
	for _, pem := range []string{p.CACert, p.AgentCert, p.AgentKey} {
		if strings.Contains(pem, "NASNET_PEM_EOF") {
			return "", errors.New("certificate contains the heredoc marker")
		}
	}
	var buf bytes.Buffer
	if err := installTemplate.Execute(&buf, p); err != nil {
		return "", err
	}
	return buf.String(), nil
}
