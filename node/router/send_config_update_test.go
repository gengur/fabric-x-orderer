/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/
package router

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hyperledger/fabric-x-orderer/common/tools/armageddon"
	"github.com/hyperledger/fabric-x-orderer/testutil"
	"github.com/hyperledger/fabric-x-orderer/testutil/client"
	"github.com/hyperledger/fabric-x-orderer/testutil/fabric/configutil"
	"github.com/onsi/gomega/gexec"
	"github.com/stretchr/testify/assert"

	"github.com/hyperledger/fabric-x-orderer/common/utils"
	fabricx_config "github.com/hyperledger/fabric-x-orderer/config"
	"github.com/hyperledger/fabric-x-orderer/testutil/stub"
	"github.com/stretchr/testify/require"
)

// TestRouterSendConfigUpdateToConsenterStub tests the end-to-end flow of sending a configuration
// update transaction through the router to a consenter stub.
func TestRouterSendConfigUpdateToConsenterStub(t *testing.T) {
	// 1. Compile arma
	armaBinaryPath, err := gexec.BuildWithEnvironment("github.com/hyperledger/fabric-x-orderer/cmd/arma", []string{"GOPRIVATE=" + os.Getenv("GOPRIVATE")})
	defer gexec.CleanupBuildArtifacts()
	require.NoError(t, err)
	require.NotNil(t, armaBinaryPath)

	// 2. Create a temporary directory for the test.
	dir, err := os.MkdirTemp("", t.Name())
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	numOfParties := 1
	numOfShards := 1
	submittingParty := 1

	// 3. Create a config YAML file in the temporary directory.
	configPath := filepath.Join(dir, "config.yaml")
	netInfo := testutil.CreateNetwork(t, configPath, numOfParties, numOfShards, "TLS", "none")
	require.NoError(t, err)

	// 4. Generate the config files in the temporary directory using the armageddon generate command.
	armageddon.NewCLI().Run([]string{"generate", "--config", configPath, "--output", dir})

	configStoreDir := t.TempDir()
	defer os.RemoveAll(configStoreDir)

	// 5. Launch the batcher node stub
	batcher := stub.NewStubBatcherFromConfig(t, configStoreDir, filepath.Join(dir, "config", "party1", "local_config_batcher1.yaml"), netInfo["Party13batcher1"].Listener)
	batcher.Start()
	defer batcher.Stop()

	// 6. Launch the router node
	readyChan := make(chan struct{}, 1)

	routerNodeInfo := netInfo["Party14router"]
	routerNodeConfigPath := filepath.Join(dir, "config", "party1", "local_config_router.yaml")
	localConfig, _, err := fabricx_config.LoadLocalConfig(routerNodeConfigPath)
	require.NoError(t, err)

	localConfig.NodeLocalConfig.FileStore.Path = configStoreDir
	localConfig.NodeLocalConfig.GeneralConfig.ClientSignatureVerificationRequired = true
	utils.WriteToYAML(localConfig.NodeLocalConfig, routerNodeConfigPath)

	testutil.RunArmaNodes(t, dir, armaBinaryPath, readyChan, map[string]*testutil.ArmaNodeInfo{"Party14router": routerNodeInfo})
	testutil.WaitReady(t, readyChan, 1, 10)

	// 7. Launch the consenter node stub
	consenterStub := stub.NewStubConsenterFromConfig(t, configStoreDir, filepath.Join(dir, "config", "party1", "local_config_consenter.yaml"), netInfo["Party11consensus"].Listener)
	consenterStub.Start()
	defer consenterStub.Stop()

	// 8. Create a broadcast client
	uc, err := testutil.GetUserConfig(dir, 1)
	assert.NoError(t, err)
	assert.NotNil(t, uc)

	broadcastClient := client.NewBroadcastTxClient(uc, 10*time.Second)
	defer broadcastClient.Stop()

	// 9. Prepare a Config TX, i.e. an envelope signed by an admin of org1
	// the envelope.Payload contains marshaled bytes of configUpdateEnvelope, which is an envelope with Header.Type = HeaderType_CONFIG_UPDATE, signed by majority of admins
	// Create the config transaction
	genesisBlockPath := filepath.Join(dir, "bootstrap/bootstrap.block")
	env := configutil.CreateConfigTX(t, dir, numOfParties, genesisBlockPath, submittingParty)
	require.NotNil(t, env)

	// 10. Send the config tx
	err = broadcastClient.SendTx(env)
	require.ErrorContains(t, err, "INTERNAL_SERVER_ERROR, Info: dummy submit config")

	require.Eventually(t, func() bool {
		return consenterStub.ReceivedMessageCount() == 1
	}, 10*time.Second, 100*time.Millisecond)
}
