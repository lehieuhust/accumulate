package api_test

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"gitlab.com/accumulatenetwork/accumulate/internal/api/v2"
	acctesting "gitlab.com/accumulatenetwork/accumulate/internal/testing"
	"gitlab.com/accumulatenetwork/accumulate/internal/testing/e2e"
	"gitlab.com/accumulatenetwork/accumulate/internal/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
	. "gitlab.com/accumulatenetwork/accumulate/protocol"
	query2 "gitlab.com/accumulatenetwork/accumulate/types/api/query"
)

func init() { acctesting.EnableDebugFeatures() }

func TestEndToEnd(t *testing.T) {
	acctesting.SkipCI(t, "flaky")
	acctesting.SkipPlatform(t, "windows", "flaky")
	acctesting.SkipPlatform(t, "darwin", "flaky")
	acctesting.SkipPlatformCI(t, "darwin", "requires setting up localhost aliases")
	t.Skip("flaky")
	suite.Run(t, e2e.NewSuite(func(s *e2e.Suite) e2e.DUT {
		subnets, daemons := acctesting.CreateTestNet(s.T(), 1, 2, 0)
		acctesting.RunTestNet(s.T(), subnets, daemons)
		return &e2eDUT{s, daemons[protocol.Directory][0]}
	}))
}

func TestValidate(t *testing.T) {
	acctesting.SkipCI(t, "flaky")
	acctesting.SkipPlatform(t, "windows", "flaky")
	acctesting.SkipPlatform(t, "darwin", "flaky")
	acctesting.SkipPlatformCI(t, "darwin", "requires setting up localhost aliases")
	t.Skip("flaky")
	subnets, daemons := acctesting.CreateTestNet(t, 2, 2, 0)
	acctesting.RunTestNet(t, subnets, daemons)
	japi := daemons[protocol.Directory][0].Jrpc_TESTONLY()

	t.Run("Not found", func(t *testing.T) {
		b, err := json.Marshal(&api.TxnQuery{Txid: make([]byte, 32), Wait: 2 * time.Second})
		require.NoError(t, err)

		r := japi.GetMethod("query-tx")(context.Background(), b)
		err, _ = r.(error)
		require.Error(t, err)
	})

	var liteKey ed25519.PrivateKey
	var liteUrl *url.URL
	t.Run("Faucet", func(t *testing.T) {
		liteKey = newKey([]byte(t.Name()))
		liteUrl = makeLiteUrl(t, liteKey, ACME)

		const count = 3
		for i := 0; i < count; i++ {
			xr := new(api.TxResponse)
			callApi(t, japi, "faucet", &AcmeFaucet{Url: liteUrl}, xr)
			require.Zero(t, xr.Code, xr.Message)
			txWait(t, japi, xr.TransactionHash)
		}

		account := new(LiteTokenAccount)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: liteUrl}, account)
		assert.Equal(t, int64(count*protocol.AcmeFaucetAmount*AcmePrecision), account.Balance.Int64())
	})

	t.Run("Lite Token Account Credits", func(t *testing.T) {
		executeTx(t, japi, "add-credits", true, execParams{
			Origin: liteUrl.String(),
			Key:    liteKey,
			Payload: &AddCredits{
				Recipient: liteUrl,
				Amount:    *big.NewInt(1e10),
			},
		})

		account := new(LiteTokenAccount)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: liteUrl}, account)
		assert.Equal(t, uint64(1e5), account.CreditBalance)

		queryRecord(t, japi, "query-chain", &api.ChainIdQuery{ChainId: liteUrl.AccountID()})
	})

	var adiKey ed25519.PrivateKey
	var adiName = &url.URL{Authority: "keytest"}
	t.Run("Create ADI", func(t *testing.T) {
		adiKey = newKey([]byte(t.Name()))

		bookUrl, err := url.Parse(fmt.Sprintf("%s/book", adiName))
		require.NoError(t, err)

		executeTx(t, japi, "create-adi", true, execParams{
			Origin: liteUrl.String(),
			Key:    liteKey,
			Payload: &CreateIdentity{
				Url:        adiName,
				KeyHash:    adiKey[32:],
				KeyBookUrl: bookUrl,
			},
		})

		adi := new(protocol.ADI)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: adiName}, adi)
		assert.Equal(t, adiName, adi.Url)

		dir := new(api.MultiResponse)
		callApi(t, japi, "query-directory", struct {
			Url          string
			Count        int
			ExpandChains bool
		}{adiName.String(), 10, true}, dir)
		assert.ElementsMatch(t, []interface{}{
			adiName.String(),
			adiName.JoinPath("/book").String(),
			adiName.JoinPath("/page").String(),
		}, dir.Items)
	})

	t.Run("Key page credits", func(t *testing.T) {
		pageUrl := adiName.JoinPath("/page")
		executeTx(t, japi, "add-credits", true, execParams{
			Origin: liteUrl.String(),
			Key:    liteKey,
			Payload: &AddCredits{
				Recipient: pageUrl,
				Amount:    *big.NewInt(1e5),
			},
		})

		page := new(KeyPage)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: pageUrl}, page)
		assert.Equal(t, uint64(1e5), page.CreditBalance)
	})

	t.Run("Txn History", func(t *testing.T) {
		r := new(api.MultiResponse)
		callApi(t, japi, "query-tx-history", struct {
			Url   string
			Count int
		}{liteUrl.String(), 10}, r)
		require.Equal(t, 7, len(r.Items), "Expected 7 transactions for %s", liteUrl)
	})

	dataAccountUrl := adiName.JoinPath("/dataAccount")
	t.Run("Create Data Account", func(t *testing.T) {
		executeTx(t, japi, "create-data-account", true, execParams{
			Origin: adiName.String(),
			Key:    adiKey,
			Payload: &CreateDataAccount{
				Url: dataAccountUrl,
			},
		})
		dataAccount := new(DataAccount)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: dataAccountUrl}, dataAccount)
		assert.Equal(t, dataAccountUrl, dataAccount.Url)
	})

	keyBookUrl := adiName.JoinPath("/book1")
	t.Run("Create Key Book", func(t *testing.T) {
		executeTx(t, japi, "create-key-book", true, execParams{
			Origin: adiName.String(),
			Key:    adiKey,
			Payload: &CreateKeyBook{
				Url: keyBookUrl,
			},
		})
		keyBook := new(KeyBook)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: keyBookUrl}, keyBook)
		assert.Equal(t, keyBookUrl, keyBook.Url)
	})

	keyPageUrl := protocol.FormatKeyPageUrl(keyBookUrl, 0)
	t.Run("Create Key Page", func(t *testing.T) {
		var keys []*KeySpecParams
		// pubKey, _ := json.Marshal(adiKey.Public())
		keys = append(keys, &KeySpecParams{
			KeyHash: adiKey[32:],
		})
		executeTx(t, japi, "create-key-page", true, execParams{
			Origin: keyBookUrl.String(),
			Key:    adiKey,
			Payload: &CreateKeyPage{
				Keys: keys,
			},
		})

		keyPage := new(KeyPage)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: keyPageUrl}, keyPage)
		assert.Equal(t, keyPageUrl, keyPage.Url)
	})

	t.Run("Key page credits 2", func(t *testing.T) {
		executeTx(t, japi, "add-credits", true, execParams{
			Origin: liteUrl.String(),
			Key:    liteKey,
			Payload: &AddCredits{
				Recipient: keyPageUrl,
				Amount:    *big.NewInt(1e5),
			},
		})

		page := new(KeyPage)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: keyPageUrl}, page)
		assert.Equal(t, uint64(1e5), page.CreditBalance)
	})

	var adiKey2 ed25519.PrivateKey
	t.Run("Update Key Page", func(t *testing.T) {
		adiKey2 = newKey([]byte(t.Name()))

		executeTx(t, japi, "update-key-page", true, execParams{
			Origin: keyPageUrl.String(),
			Key:    adiKey,
			Payload: &UpdateKeyPage{
				Operation: []protocol.KeyPageOperation{&AddKeyOperation{
					Entry: KeySpecParams{
						KeyHash: adiKey2[32:],
						Owner:   makeUrl(t, "acc://foo/book1"),
					},
				}},
			},
		})
		keyPage := new(KeyPage)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: keyPageUrl}, keyPage)
		assert.Equal(t, "acc://foo/book1", keyPage.Keys[1].Owner.String())
	})

	tokenAccountUrl := adiName.JoinPath("/account")
	t.Run("Create Token Account", func(t *testing.T) {
		executeTx(t, japi, "create-token-account", true, execParams{
			Origin: adiName.String(),
			Key:    adiKey,
			Payload: &CreateTokenAccount{
				Url:        tokenAccountUrl,
				TokenUrl:   protocol.AcmeUrl(),
				KeyBookUrl: keyBookUrl,
			},
		})
		tokenAccount := new(LiteTokenAccount)
		queryRecordAs(t, japi, "query", &api.UrlQuery{Url: tokenAccountUrl}, tokenAccount)
		assert.Equal(t, tokenAccountUrl, tokenAccount.Url)
	})

	t.Run("Query Key Index", func(t *testing.T) {
		keyIndex := &query2.ResponseKeyPageIndex{}
		queryRecordAs(t, japi, "query-key-index", &api.KeyPageIndexQuery{
			UrlQuery: api.UrlQuery{
				Url: keyPageUrl,
			},
			Key: adiKey[32:],
		}, keyIndex)
		assert.Equal(t, keyPageUrl, keyIndex.Signer)
	})
}

func TestTokenTransfer(t *testing.T) {
	t.Skip("Broken")
	acctesting.SkipPlatform(t, "windows", "flaky")
	acctesting.SkipPlatform(t, "darwin", "flaky")
	acctesting.SkipPlatformCI(t, "darwin", "requires setting up localhost aliases")

	subnets, daemons := acctesting.CreateTestNet(t, 2, 2, 0)
	acctesting.RunTestNet(t, subnets, daemons)

	var aliceKey ed25519.PrivateKey
	var aliceUrl *url.URL
	var bobKey ed25519.PrivateKey
	var bobUrl *url.URL
	t.Run("Send Token", func(t *testing.T) {
		bobKey = newKey([]byte(t.Name()))
		bobUrl = makeLiteUrl(t, bobKey, ACME)
		aliceKey = newKey([]byte(t.Name()))
		aliceUrl = makeLiteUrl(t, aliceKey, ACME)

		var to []*protocol.TokenRecipient
		to = append(to, &protocol.TokenRecipient{
			Url:    aliceUrl,
			Amount: *big.NewInt(100),
		})
		txParams := execParams{
			Origin: bobUrl.String(),
			Key:    bobKey,
			Payload: &protocol.SendTokens{
				To: to,
			},
		}

		// Ensure we see the not found error code regardless of which
		// node on which BVN the transaction is sent to
		for netName, daemons := range daemons {
			if netName == protocol.Directory {
				continue
			}
			for i, daemon := range daemons {
				japi := daemon.Jrpc_TESTONLY()
				res := executeTxFail(t, japi, "send-tokens", bobUrl, 1, txParams)
				code := res.Result.(map[string]interface{})["code"].(float64)
				assert.Equal(t, protocol.ErrorCodeNotFound, protocol.ErrorCode(code), "Node %d (%s) returned the wrong error code", i, netName)
			}
		}
	})

}
