package starlink

import pb "github.com/nasnet-community/nasnet-panel-linux/pkg/starlink/pb"

// PassthroughAllowed reports whether the agent may forward req to the dish:
// reads plus the actions the panel exposes. Trusted keys, IQ capture, test
// modes and the rest stay off the wire.
func PassthroughAllowed(req *pb.Request) bool {
	switch req.GetRequest().(type) {
	case *pb.Request_GetStatus, *pb.Request_GetHistory, *pb.Request_GetDeviceInfo,
		*pb.Request_GetDiagnostics, *pb.Request_DishGetConfig, *pb.Request_DishGetObstructionMap,
		*pb.Request_GetLocation, *pb.Request_DishGetContext,
		*pb.Request_Reboot, *pb.Request_DishStow, *pb.Request_DishClearObstructionMap,
		*pb.Request_SelfTest, *pb.Request_DishInhibitGps, *pb.Request_DishFactoryReset,
		*pb.Request_DishSetConfig:
		return true
	}
	return false
}
