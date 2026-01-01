package telegram

import (
	"strings"

	"github.com/nasnet-community/nasnet-panel-linux/internal/node/usecase"
	sniUC "github.com/nasnet-community/nasnet-panel-linux/internal/sni/usecase"
	"github.com/nasnet-community/nasnet-panel-linux/pkg/conversation"
)

type Handler struct {
	nodeUsecase  usecase.NodeUsecase
	sniUsecase   sniUC.SNIUsecase
	stateManager *conversation.StateManager
}

func NewHandler(nodeUsecase usecase.NodeUsecase, sniUsecase sniUC.SNIUsecase, stateManager *conversation.StateManager) *Handler {
	return &Handler{
		nodeUsecase:  nodeUsecase,
		sniUsecase:   sniUsecase,
		stateManager: stateManager,
	}
}

func getFlag(cc string) string {
	if len(cc) != 2 {
		return "🌍"
	}
	cc = strings.ToUpper(cc)
	return string(rune(cc[0])+127397) + string(rune(cc[1])+127397)
}
