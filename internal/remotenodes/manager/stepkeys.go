// SPDX-License-Identifier: AGPL-3.0-or-later

package manager

import "github.com/nasnet-community/nasnet-panel-linux/internal/remotenodes/agentops"

// stepKeys is the machine key of every step a job can show, by the English name
// the step goes by. The page words a step by its key and shows the name only for
// a key it has no words for, so a key is never changed or reused once a build has
// sent it. Every step needs one: stepkeys_test.go fails for a step constant
// that is missing here.
var stepKeys = map[string]string{
	agentops.StepCheckServer:   "check_server",
	agentops.StepDownloadAgent: "download_agent",
	agentops.StepCertificate:   "create_certificate",
	agentops.StepPrepareServer: "prepare_server",
	agentops.StepUploadAgent:   "upload_agent",
	agentops.StepStartAgent:    "start_agent",
	agentops.StepCheckAgent:    "check_agent",
	agentops.StepStopAgent:     "stop_agent",
	agentops.StepRemoveAgent:   "remove_agent",
	agentops.StepRestartAgent:  "restart_agent",
	agentops.StepDownloadXray:  "download_xray",
	agentops.StepUploadXray:    "upload_xray",
	agentops.StepCheckXray:     "check_xray",
	agentops.StepChooseTarget:  "choose_target",
	agentops.StepCreateKeys:    "create_keys",
	agentops.StepApplyConfig:   "apply_config",
	agentops.StepCleanServer:   "clean_server",
	agentops.StepTakeOverXray:  "take_over_xray",

	StepConnect:     "connect_agent",
	StepSaveNode:    "save_node",
	StepRenewCert:   "renew_cert",
	StepForgetNode:  "forget_node",
	StepDiscardNode: "discard_node",
}

// StepKey is the key of the step with this name, and "" for a name that has
// none. It is what the job engine is given as Config.StepKey.
func StepKey(name string) string { return stepKeys[name] }
