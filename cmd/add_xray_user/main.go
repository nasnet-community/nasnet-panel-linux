package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nasnet-community/nasnet-panel-linux/pkg/xray"
)

func main() {
	apiAddress := "127.0.0.1:8080"
	client := xray.NewGRPCClient(10) // timeout in seconds

	targetInboundTag := "inbound-5552"

	log.Printf("Connecting to Xray at %s...", apiAddress)

	ctx := context.Background()
	inbounds, err := client.ListInbounds(ctx, apiAddress, false)
	if err != nil {
		log.Fatalf("Failed to list inbounds: %v", err)
	}

	var targetInbound *xray.InboundInfo
	for _, in := range inbounds {
		if in.Tag == targetInboundTag {
			targetInbound = in
			break
		}
	}

	if targetInbound == nil {
		log.Fatalf("Inbound with tag '%s' not found", targetInboundTag)
	}

	log.Printf("Found inbound '%s' with protocol '%s'", targetInbound.Tag, targetInbound.Protocol)

	var protocol xray.Protocol
	switch strings.ToLower(targetInbound.Protocol) {
	case "vmess":
		protocol = xray.ProtocolVMess
	case "vless":
		protocol = xray.ProtocolVLESS
	case "trojan":
		protocol = xray.ProtocolTrojan
	default:
		log.Fatalf("Unsupported or unknown protocol: %s", targetInbound.Protocol)
	}

	uuid := xray.GenerateUUID()
	email := fmt.Sprintf("user_%s_%d", strings.ReplaceAll(targetInboundTag, "-", "_"), time.Now().Unix())

	user := &xray.User{
		Email:    email,
		UUID:     uuid,
		Level:    0,
		Protocol: protocol,
		AlterId:  0,  // Default for vmess
		Flow:     "", // Default empty
	}

	if protocol == xray.ProtocolVLESS {
		user.Flow = "xtls-rprx-vision"
	}

	log.Printf("Adding user: Email=%s, UUID=%s, Protocol=%s", user.Email, user.UUID, user.Protocol)
	if err := client.AddUser(ctx, apiAddress, targetInboundTag, user); err != nil {
		log.Fatalf("Failed to add user: %v", err)
	}

	fmt.Println("---------------------------------------------------")
	fmt.Printf("Successfully created user in inbound '%s'\n", targetInboundTag)
	fmt.Printf("Email:    %s\n", user.Email)
	fmt.Printf("UUID:     %s\n", user.UUID)
	fmt.Printf("Protocol: %s\n", user.Protocol)
	fmt.Println("---------------------------------------------------")
}
