/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package testutil

import (
	"fmt"
	"net"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/hyperledger/fabric-x-orderer/common/types"
	"github.com/onsi/gomega/gexec"
	"github.com/stretchr/testify/require"
)

const (
	TerminationGracePeriod = 10 * time.Second
)

// var NodeSortingTable = map[NodeType]int{Router: 0, Batcher: 1, Consensus: 2, Assembler: 3}

type ArmaNetwork struct {
	armaNodes map[NodeType][][]*ArmaNodeInfo
}

type ArmaNodeRunInfo struct {
	ArmaBinaryPath string
	NodeConfigPath string
	Session        *gexec.Session
}

type ArmaNodeInfo struct {
	RunInfo         *ArmaNodeRunInfo
	NodeType        NodeType
	Listener        net.Listener
	PartyId         types.PartyID
	ShardId         types.ShardID
	ConfigBlockPath string
}

func (armaNetwork *ArmaNetwork) AddArmaNode(nodeType NodeType, partyIdx int, nodeInfo *ArmaNodeInfo) {
	if nodeType == Batcher && len(armaNetwork.armaNodes[Batcher]) > partyIdx {
		armaNetwork.armaNodes[Batcher][partyIdx] = append(armaNetwork.armaNodes[Batcher][partyIdx], nodeInfo)
	} else {
		armaNetwork.armaNodes[nodeType] = append(armaNetwork.armaNodes[nodeType], []*ArmaNodeInfo{nodeInfo})
	}
}

func (armaNetwork *ArmaNetwork) Stop() {
	for _, k := range []NodeType{Assembler, Consensus, Batcher, Router} {
		for i := range armaNetwork.armaNodes[k] {
			for j := range armaNetwork.armaNodes[k][i] {
				armaNetwork.armaNodes[k][i][j].StopArmaNode()
			}
		}
	}
}

func (armaNetwork *ArmaNetwork) Kill() {
	for _, k := range []NodeType{Assembler, Consensus, Batcher, Router} {
		for i := range armaNetwork.armaNodes[k] {
			for j := range armaNetwork.armaNodes[k][i] {
				armaNetwork.armaNodes[k][i][j].KillArmaNode()
			}
		}
	}
}

func (armaNetwork *ArmaNetwork) Restart(t *testing.T, readyChan chan string) {
	for _, k := range []NodeType{Assembler, Consensus, Batcher, Router} {
		for i := range armaNetwork.armaNodes[k] {
			for j := range armaNetwork.armaNodes[k][i] {
				armaNetwork.armaNodes[k][i][j].RestartArmaNode(t, readyChan)
			}
		}
	}
}

func (armaNetwork *ArmaNetwork) StopParties(t *testing.T, parties []types.PartyID) {
	for _, k := range []NodeType{Assembler, Consensus, Batcher, Router} {
		for _, partyID := range parties {
			partyIdx := slices.IndexFunc(armaNetwork.armaNodes[k], func(nodes []*ArmaNodeInfo) bool {
				return nodes[0].PartyId == partyID
			})
			require.True(t, partyIdx >= 0, fmt.Sprintf("%s for party %d not found", k, partyID))
			for j := range armaNetwork.armaNodes[k][partyIdx] {
				armaNetwork.armaNodes[k][partyIdx][j].StopArmaNode()
			}
		}
	}
}

func (armaNetwork *ArmaNetwork) RestartParties(t *testing.T, parties []types.PartyID, readyChan chan string) {
	for _, k := range []NodeType{Assembler, Consensus, Batcher, Router} {
		for _, partyID := range parties {
			partyIdx := slices.IndexFunc(armaNetwork.armaNodes[k], func(nodes []*ArmaNodeInfo) bool {
				return nodes[0].PartyId == partyID
			})
			require.True(t, partyIdx >= 0, fmt.Sprintf("%s for party %d not found", k, partyID))
			for j := range armaNetwork.armaNodes[k][partyIdx] {
				armaNetwork.armaNodes[k][partyIdx][j].RestartArmaNode(t, readyChan)
			}
		}
	}
}

func (armaNetwork *ArmaNetwork) GetAssembler(t *testing.T, partyID types.PartyID) *ArmaNodeInfo {
	require.True(t, int(partyID) > 0)
	partyIdx := slices.IndexFunc(armaNetwork.armaNodes[Assembler], func(nodes []*ArmaNodeInfo) bool {
		return nodes[0].PartyId == partyID
	})
	require.True(t, partyIdx >= 0, fmt.Sprintf("assembler for party %d not found", partyID))
	return armaNetwork.armaNodes[Assembler][partyIdx][0]
}

func (armaNetwork *ArmaNetwork) GetRouter(t *testing.T, partyID types.PartyID) *ArmaNodeInfo {
	require.True(t, int(partyID) > 0)
	partyIdx := slices.IndexFunc(armaNetwork.armaNodes[Router], func(nodes []*ArmaNodeInfo) bool {
		return nodes[0].PartyId == partyID
	})
	require.True(t, partyIdx >= 0, fmt.Sprintf("router for party %d not found", partyID))
	return armaNetwork.armaNodes[Router][partyIdx][0]
}

func (armaNetwork *ArmaNetwork) GetConsenter(t *testing.T, partyID types.PartyID) *ArmaNodeInfo {
	require.True(t, int(partyID) > 0)
	partyIdx := slices.IndexFunc(armaNetwork.armaNodes[Consensus], func(nodes []*ArmaNodeInfo) bool {
		return nodes[0].PartyId == partyID
	})
	require.True(t, partyIdx >= 0, fmt.Sprintf("consenter for party %d not found", partyID))
	return armaNetwork.armaNodes[Consensus][partyIdx][0]
}

func (armaNetwork *ArmaNetwork) GetBatcher(t *testing.T, partyID types.PartyID, shardID types.ShardID) *ArmaNodeInfo {
	require.True(t, int(partyID) > 0)
	require.True(t, int(shardID) > 0)
	partyIdx := slices.IndexFunc(armaNetwork.armaNodes[Batcher], func(nodes []*ArmaNodeInfo) bool {
		return nodes[0].PartyId == partyID
	})
	require.True(t, partyIdx >= 0, fmt.Sprintf("batcher for party %d not found", partyID))
	require.True(t, len(armaNetwork.armaNodes[Batcher][partyIdx]) >= int(shardID))
	return armaNetwork.armaNodes[Batcher][partyIdx][shardID-1]
}

func (armaNodeInfo *ArmaNodeInfo) RestartArmaNode(t *testing.T, readyChan chan string) {
	require.FileExists(t, armaNodeInfo.RunInfo.NodeConfigPath)
	nodeConfig := ReadNodeConfigFromYaml(t, armaNodeInfo.RunInfo.NodeConfigPath)
	storagePath := nodeConfig.FileStore.Path
	require.DirExists(t, storagePath)

	armaNodeInfo.RunInfo.Session = runNode(t, armaNodeInfo.NodeType.String(), armaNodeInfo.RunInfo.ArmaBinaryPath,
		armaNodeInfo.RunInfo.NodeConfigPath, readyChan, armaNodeInfo.Listener)
}

func (armaNodeInfo *ArmaNodeInfo) StopArmaNode() {
	select {
	case <-armaNodeInfo.RunInfo.Session.Terminate().Exited:
	case <-time.After(TerminationGracePeriod):
		fmt.Fprintf(os.Stderr, "Graceful shutdown: timeout expired Party%d%s@%s is about to be killed", armaNodeInfo.PartyId, armaNodeInfo.NodeType, armaNodeInfo.Listener.Addr())
		<-armaNodeInfo.RunInfo.Session.Kill().Exited
	}
}

func (armaNodeInfo *ArmaNodeInfo) KillArmaNode() {
	<-armaNodeInfo.RunInfo.Session.Kill().Exited
}
