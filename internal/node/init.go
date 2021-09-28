package node

import (
	"context"
	"fmt"
	"os"
	"path"

	cfg "github.com/AccumulateNetwork/accumulated/config"
	"github.com/AccumulateNetwork/accumulated/networks"
	tmcfg "github.com/tendermint/tendermint/config"
	tmlog "github.com/tendermint/tendermint/libs/log"
	tmos "github.com/tendermint/tendermint/libs/os"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmtime "github.com/tendermint/tendermint/libs/time"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
)

const (
	nodeDirPerm = 0755
)

// InitForNetwork creates the initial configuration for a set of nodes for the
// given network.
func InitForNetwork(shardname string, index int, WorkingDir string) {
	network := networks.Networks[index]

	listenIP := make([]string, len(network.Ip))
	config := make([]*cfg.Config, len(network.Ip))

	for i := range network.Ip {
		listenIP[i] = "tcp://0.0.0.0"
		config[i] = new(cfg.Config)
		config[i].Config = *tmcfg.DefaultValidatorConfig()
	}

	err := InitWithConfig(WorkingDir, shardname, network.Name, network.Port, config, network.Ip, listenIP)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize: %v\n", err)
	}
}

// InitWithConfig creates the initial configuration for a set of nodes, using
// the given configuration. Config, remoteIP, and listenIP must all be of equal
// length.
func InitWithConfig(workDir, shardName, chainID string, port int, config []*cfg.Config, remoteIP []string, listenIP []string) (err error) {
	defer func() {
		if err != nil {
			_ = os.RemoveAll(workDir)
		}
	}()

	fmt.Println("Tendermint Initialize")

	genVals := make([]types.GenesisValidator, 0, len(config))

	for i, config := range config {
		nodeDirName := fmt.Sprintf("Node%d", i)
		nodeDir := path.Join(workDir, nodeDirName)
		config.SetRoot(nodeDir)

		config.Instrumentation.Namespace = shardName

		// config.ProxyApp = fmt.Sprintf("%s:%d", IPs[i], port)
		config.ProxyApp = ""
		config.P2P.ListenAddress = fmt.Sprintf("%s:%d", listenIP[i], port)
		config.RPC.ListenAddress = fmt.Sprintf("%s:%d", listenIP[i], port+1)
		config.RPC.GRPCListenAddress = fmt.Sprintf("%s:%d", listenIP[i], port+2)
		config.Instrumentation.PrometheusListenAddr = fmt.Sprintf(":%d", port)

		err = os.MkdirAll(path.Join(nodeDir, "config"), nodeDirPerm)
		if err != nil {
			return fmt.Errorf("failed to create config dir: %v", err)
		}

		err = os.MkdirAll(path.Join(nodeDir, "data"), nodeDirPerm)
		if err != nil {
			return fmt.Errorf("failed to create data dir: %v", err)
		}

		if err := initFilesWithConfig(config, &chainID); err != nil {
			return err
		}

		pvKeyFile := path.Join(nodeDir, config.PrivValidator.Key)
		pvStateFile := path.Join(nodeDir, config.PrivValidator.State)
		pv, err := privval.LoadFilePV(pvKeyFile, pvStateFile)
		if err != nil {
			return fmt.Errorf("failed to load private validator: %v", err)
		}

		pubKey, err := pv.GetPubKey(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get public key: %v", err)
		}

		if config.Mode == tmcfg.ModeValidator {
			genVals = append(genVals, types.GenesisValidator{
				Address: pubKey.Address(),
				PubKey:  pubKey,
				Power:   1,
				Name:    nodeDirName,
			})
		}
	}

	// Generate genesis doc from generated validators
	genDoc := &types.GenesisDoc{
		ChainID:         "chain-" + tmrand.Str(6),
		GenesisTime:     tmtime.Now(),
		InitialHeight:   0,
		Validators:      genVals,
		ConsensusParams: types.DefaultConsensusParams(),
	}

	// Write genesis file.
	for _, config := range config {
		if err := genDoc.SaveAs(path.Join(config.RootDir, config.BaseConfig.Genesis)); err != nil {
			return fmt.Errorf("failed to save gen doc: %v", err)
		}
	}

	// Gather validator peer addresses.
	validatorPeers := map[int]string{}
	for i, config := range config {
		// if config.Mode != tmcfg.ModeValidator {
		// 	continue
		// }

		nodeKey, err := types.LoadNodeKey(config.NodeKeyFile())
		if err != nil {
			return fmt.Errorf("failed to load node key: %v", err)
		}
		validatorPeers[i] = nodeKey.ID.AddressString(fmt.Sprintf("%s:%d", remoteIP[i], port))
	}

	// Overwrite default config.
	nConfig := len(config)
	for i, config := range config {
		if nConfig > 1 {
			config.P2P.AddrBookStrict = false
			config.P2P.AllowDuplicateIP = true
			config.P2P.PersistentPeers = ""
			for j, peer := range validatorPeers {
				if j != i {
					config.P2P.PersistentPeers += "," + peer
				}
			}
			config.P2P.PersistentPeers = config.P2P.PersistentPeers[1:]
		} else {
			config.P2P.AddrBookStrict = true
			config.P2P.AllowDuplicateIP = false
		}
		config.Moniker = fmt.Sprintf("Node%d", i)

		config.Accumulate.AccRPC.ListenAddress = fmt.Sprintf("%s:%d", listenIP[i], port+3)
		config.Accumulate.AccRouter.JSONListenAddress = fmt.Sprintf("%s:%d", listenIP[i], port+4)
		config.Accumulate.AccRouter.RESTListenAddress = fmt.Sprintf("%s:%d", listenIP[i], port+5)

		err := cfg.Store(config)
		if err != nil {
			return err
		}
	}

	switch nValidators := len(genVals); nValidators {
	case 0:
		fmt.Printf("Successfully initialized %v follower nodes\n", nConfig)
	case nConfig:
		fmt.Printf("Successfully initialized %v validator nodes\n", nConfig)
	default:
		fmt.Printf("Successfully initialized %v validator and %v follower nodes\n", nValidators, nConfig-nValidators)
	}
	return nil
}

func initFilesWithConfig(config *cfg.Config, chainid *string) error {

	logger := tmlog.NewNopLogger()

	// private validator
	privValKeyFile := config.PrivValidator.KeyFile()
	privValStateFile := config.PrivValidator.StateFile()
	var pv *privval.FilePV
	var err error
	if tmos.FileExists(privValKeyFile) {
		pv, err = privval.LoadFilePV(privValKeyFile, privValStateFile)
		if err != nil {
			return fmt.Errorf("failed to load private validator: %w", err)
		}
		logger.Info("Found private validator", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	} else {
		pv, err = privval.GenFilePV(privValKeyFile, privValStateFile, "")
		if err != nil {
			return fmt.Errorf("failed to gen private validator: %w", err)
		}
		pv.Save()
		logger.Info("Generated private validator", "keyFile", privValKeyFile,
			"stateFile", privValStateFile)
	}

	nodeKeyFile := config.NodeKeyFile()
	if tmos.FileExists(nodeKeyFile) {
		logger.Info("Found node key", "path", nodeKeyFile)
	} else {
		if _, err := types.LoadOrGenNodeKey(nodeKeyFile); err != nil {
			return fmt.Errorf("can't load or gen node key: %v", err)
		}
		logger.Info("Generated node key", "path", nodeKeyFile)
	}
	// genesis file
	genFile := config.GenesisFile()
	if tmos.FileExists(genFile) {
		logger.Info("Found genesis file", "path", genFile)
	} else {
		genDoc := types.GenesisDoc{
			ChainID:         *chainid,
			GenesisTime:     tmtime.Now(),
			ConsensusParams: types.DefaultConsensusParams(),
		}
		pubKey, err := pv.GetPubKey(context.Background())
		if err != nil {
			return fmt.Errorf("can't get pubkey: %v", err)
		}
		genDoc.Validators = []types.GenesisValidator{{
			Address: pubKey.Address(),
			PubKey:  pubKey,
			Power:   10,
		}}

		if err := genDoc.SaveAs(genFile); err != nil {
			return fmt.Errorf("can't save genFile: %s: %v", genFile, err)
		}
		logger.Info("Generated genesis file", "path", genFile)
	}

	return nil
}
