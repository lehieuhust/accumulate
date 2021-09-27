package router

import (
	"fmt"

	"github.com/AccumulateNetwork/accumulated/blockchain/validator"
	"github.com/AccumulateNetwork/accumulated/config"
	"github.com/AccumulateNetwork/accumulated/internal/abci"
	"github.com/AccumulateNetwork/accumulated/internal/node"
	"github.com/spf13/viper"
	"github.com/tendermint/tendermint/abci/types"
	tmnet "github.com/tendermint/tendermint/libs/net"
	"github.com/tendermint/tendermint/privval"
	tmdb "github.com/tendermint/tm-db"
)

func randomRouterPorts() *config.Router {
	port, err := tmnet.GetFreePort()
	if err != nil {
		panic(err)
	}
	return &config.Router{
		JSONListenAddress: fmt.Sprintf("localhost:%d", port),
		RESTListenAddress: fmt.Sprintf("localhost:%d", port+1),
	}
}

func boostrapBVC(configfile string, workingdir string, baseport int) error {
	node.InitForNetwork("accumulate.", 2, workingdir)
	viper.SetConfigFile(configfile)
	viper.AddConfigPath(workingdir + "/Node0")
	viper.ReadInConfig()
	//[mempool]
	//	broadcast = true
	//	cache_size = 100000
	//	max_batch_bytes = 10485760
	//	max_tx_bytes = 1048576
	//	max_txs_bytes = 1073741824
	//	recheck = true
	//	size = 50000
	//	wal_dir = ""
	//

	//viper.Set("mempool.keep-invalid-txs-in-cache, "false"
	//viper.Set("mempool.max_txs_bytes", "1073741824")
	viper.Set("mempool.max_batch_bytes", 1048576)
	viper.Set("mempool.cache_size", 1048576)
	viper.Set("mempool.size", 50000)
	err := viper.WriteConfig()
	if err != nil {
		panic(err)
	}

	return nil
}

func newBVC(configfile string, workingdir string) *node.Node {
	cfg, err := config.LoadFile(workingdir, configfile)
	if err != nil {
		panic(err)
	}

	db, err := tmdb.NewGoLevelDB("kvstore", workingdir)
	if err != nil {
		panic(err)
	}

	node, err := node.New(cfg, func(pv *privval.FilePV) (types.Application, error) {
		vnode := new(validator.Node)
		vchain := &validator.NewBlockValidatorChain().ValidatorContext
		err = vnode.Initialize(cfg, pv.Key.PrivKey.Bytes(), vchain)
		if err != nil {
			panic(err)
		}

		return abci.NewAccumulator(db, pv, vnode)
	})
	if err != nil {
		panic(err)
	}
	return node
}

func startBVC(cfg string, dir string) *node.Node {

	//Select a base port to open.  Ports 43210, 43211, 43212, 43213,43214 need to be open
	baseport := 35550

	//generate the config files needed to run a test BVC
	err := boostrapBVC(cfg, dir, baseport)
	if err != nil {
		panic(err)
	}

	//First we need to build a Router.  The router has to be done first since the BVC connects to it.
	//Make the router's client (i.e. Public facing GRPC client that will route the message to the correct network) and
	//server (i.e. The GRPC that will convert public GRPC messages into messages to communicate with the BVC application)
	viper.SetConfigFile(cfg)
	viper.AddConfigPath(dir)
	viper.ReadInConfig()

	///Build a BVC we'll use for our test
	node := newBVC(cfg, dir+"/Node0")
	err = node.Start()
	if err != nil {
		panic(err)
	}

	return node
}
