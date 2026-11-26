package cmd

import (
	"fmt"

	"gorm.io/gorm"

	alertDomain "github.com/nasnet-community/nasnet-panel-linux/internal/alerting/domain"
	coreDomain "github.com/nasnet-community/nasnet-panel-linux/internal/core/domain"
	nodeDomain "github.com/nasnet-community/nasnet-panel-linux/internal/node/domain"
	settingDomain "github.com/nasnet-community/nasnet-panel-linux/internal/setting/domain"
)

// MTProto nodes are gone. A panel that had some still has their rows, and
// their tunnels would keep handing out links to servers nothing drives, so
// they go at startup. The agents on those servers are left running; the
// panel can no longer reach them.
var droppedMTProtoRuleTypes = []string{"relay_allowlist_violation", "relay_egress_down", "relay_disguise_down"}

// dropMTProtoNodes deletes the MTProto nodes with their tunnels and stats,
// their alert rules, the Telegram list setting and their own tables, and
// returns how many nodes it deleted. Running it again changes nothing.
func dropMTProtoNodes(db *gorm.DB) (int, error) {
	m := db.Migrator()
	var ids []uint
	// nodes.kind was only ever "full" or "lite"; panels that never had it have no MTProto nodes.
	if m.HasColumn(&nodeDomain.Node{}, "kind") {
		if err := db.Model(&nodeDomain.Node{}).Where("kind = ?", "lite").Pluck("id", &ids).Error; err != nil {
			return 0, fmt.Errorf("list MTProto nodes: %w", err)
		}
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		if len(ids) > 0 {
			tunnels := tx.Model(&coreDomain.CoreInbound{}).Select("id").Where("node_id IN ?", ids)
			if err := tx.Where("core_inbound_id IN (?)", tunnels).Delete(&coreDomain.CoreUser{}).Error; err != nil {
				return err
			}
			if err := tx.Where("node_id IN ?", ids).Delete(&coreDomain.CoreInbound{}).Error; err != nil {
				return err
			}
			if err := tx.Where("node_id IN ?", ids).Delete(&nodeDomain.NodeStat{}).Error; err != nil {
				return err
			}
			if err := tx.Delete(&nodeDomain.Node{}, ids).Error; err != nil {
				return err
			}
		}

		rules := tx.Model(&alertDomain.Rule{}).Select("id").Where("rule_type IN ?", droppedMTProtoRuleTypes)
		if err := tx.Where("rule_id IN (?)", rules).Delete(&alertDomain.State{}).Error; err != nil {
			return err
		}
		if err := tx.Where("rule_id IN (?)", rules).Delete(&alertDomain.Event{}).Error; err != nil {
			return err
		}
		if err := tx.Where("rule_type IN ?", droppedMTProtoRuleTypes).Delete(&alertDomain.Rule{}).Error; err != nil {
			return err
		}
		return tx.Delete(&settingDomain.Setting{Key: "relay_cidr_url"}).Error
	})
	if err != nil {
		return 0, fmt.Errorf("delete MTProto nodes: %w", err)
	}

	for _, table := range []string{"lite_nodes", "lite_usage_baselines"} {
		if m.HasTable(table) {
			if err := m.DropTable(table); err != nil {
				return len(ids), fmt.Errorf("drop %s: %w", table, err)
			}
		}
	}
	return len(ids), nil
}
