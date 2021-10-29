package cmd

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	url2 "github.com/AccumulateNetwork/accumulated/internal/url"
	"github.com/AccumulateNetwork/accumulated/protocol"
	"github.com/AccumulateNetwork/accumulated/types"
	acmeapi "github.com/AccumulateNetwork/accumulated/types/api"
	"github.com/boltdb/bolt"
	"log"

	"github.com/spf13/cobra"
)

var adiCmd = &cobra.Command{
	Use:   "adi",
	Short: "Create and manage ADI",
	Run: func(cmd *cobra.Command, args []string) {

		if len(args) > 0 {
			switch arg := args[0]; arg {
			case "get":
				if len(args) > 1 {
					GetADI(args[1])
				} else {
					fmt.Println("Usage:")
					PrintADIGet()
				}
			case "list":
				ListADIs()
			case "create":
				if len(args) == 3 {
					NewADI(args[1], args[2], "", "", "")
				} else if len(args) == 4 {
					NewADI(args[1], args[2], args[3], "", "")
				} else if len(args) == 6 {
					NewADI(args[1], args[2], args[3], args[4], args[5])
				} else {
					fmt.Println("Usage:")
					PrintADICreate()
				}
			default:
				fmt.Println("Usage:")
				PrintADI()
			}
		} else {
			fmt.Println("Usage:")
			PrintADI()
		}

	},
}

func init() {
	rootCmd.AddCommand(adiCmd)
}

func PrintADIGet() {
	fmt.Println("  accumulate adi get [URL]			Get existing ADI by URL")
}

func PrintADICreate() {
	fmt.Println("  accumulate adi create [signer-url] [adi-url] [key-book-name (optional)] [key-page-name (optional)] [public-key (optional)] Create new ADI")
}

func PrintADIImport() {
	fmt.Println("  accumulate adi import [adi-url] [private-key]	Import Existing ADI")
}

func PrintADI() {
	PrintADIGet()
	PrintADICreate()
	PrintADIImport()
}

func GetADI(url string) {

	var res interface{}
	var str []byte

	params := acmeapi.APIRequestURL{}
	params.URL = types.String(url)

	if err := Client.Request(context.Background(), "adi", params, &res); err != nil {
		log.Fatal(err)
	}

	str, err := json.Marshal(res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(str))

}

//
//func PublicADI(url string) {
//
//	fmt.Println("ADI functionality is not available on Testnet")
//
//}

//func NewADIFromADISponsor()

// NewADI create a new ADI from a sponsored account.
func NewADI(sender string, adiUrl string, book string, page string, pubKeyHex string) {
	var pubKey []byte
	pubKey = make([]byte, 32)

	u, err := url2.Parse(adiUrl)
	if err != nil {
		log.Fatal(err)
	}

	_, _, err = protocol.ParseAnonymousAddress(u)
	isSponsoredByLiteAccount := IsLiteAccount(u.String())

	if len(pubKeyHex) != 64 && len(pubKeyHex) != 0 {
		log.Fatalf("invalid public key")
	}
	i, err := hex.Decode(pubKey, []byte(pubKeyHex))

	if i != 64 && i != 0 {
		log.Fatalf("invalid public key")
	}

	var privKey ed25519.PrivateKey
	if i == 0 {
		pubKey, privKey, err = ed25519.GenerateKey(nil)
		fmt.Printf("Created Initial Key %x\n", pubKey)
	}

	if book == "" {
		book = "ssg0"
		book = u.JoinPath(book).String()
	}
	if page == "" {
		page = "sigspec0"
		page = u.JoinPath(page).String()
	}

	idc := &protocol.IdentityCreate{}
	idc.Url = u.Authority
	idc.PublicKey = pubKey
	idc.KeyBookName = book
	idc.KeyPageName = page

	data, err := json.Marshal(idc)
	if err != nil {
		log.Fatal(err)
	}

	dataBinary, err := idc.MarshalBinary()
	if err != nil {
		log.Fatal(err)
	}

	bucket := "adi"
	if isSponsoredByLiteAccount {
		bucket = "anon"
	}

	params, err := prepareGenTx(data, dataBinary, sender, sender, bucket)
	if err != nil {
		log.Fatal(err)
	}

	//Store the new adi in case things go bad
	//haveData := false
	var asData []byte
	if len(privKey) != 0 {
		//as := AdiStore{}
		//
		//err = Db.View(func(tx *bolt.Tx) error {
		//	b := tx.Bucket([]byte("adi"))
		//	asData = b.Get([]byte(u.Authority))
		//	return err
		//})
		//
		//if asData != nil {
		//	haveData = true
		//	err = json.Unmarshal(asData, &as)
		//	log.Fatal(err)
		//}
		//as.KeyBooks = make(map[string]KeyBookStore)
		//if b, ok := as.KeyBooks[book]; !ok {
		//	if b.KeyPages == nil {
		//		b.KeyPages = make(map[string]KeyPageStore)
		//	}
		//	as.KeyBooks[page] = b
		//	if p, ok := b.KeyPages[page]; !ok {
		//		p.PrivKeys = append(p.PrivKeys, types.Bytes(privKey))
		//		b.KeyPages[page] = p
		//	}
		//}
		//
		//asData, err = json.Marshal(&as)
		//if err != nil {
		//	log.Fatal(err)
		//}
		//
		//if err != nil {
		//	log.Fatal(err)
		//}
	}

	var res interface{}
	var str []byte
	if err := Client.Request(context.Background(), "adi-create", params, &res); err != nil {
		//todo: if we fail, then we need to remove the adi from storage or keep it and try again later...
		//if !haveData {
		//	err = Db.Update(func(tx *bolt.Tx) error {
		//		b := tx.Bucket([]byte("adi"))
		//		err := b.Delete([]byte(adiUrl))
		//		return err
		//	})
		//}

		log.Fatal(err)
	}

	err = Db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("adi"))
		err := b.Put([]byte(adiUrl), asData)
		return err
	})

	str, err = json.Marshal(res)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(string(str))

}

func ListADIs() {

	err := Db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("adi"))
		c := b.Cursor()
		for k, v := c.First(); k != nil; k, _ = c.Next() {
			//
			//as := AdiStore{}
			//err := json.Unmarshal(v, &as)
			//if err != nil {
			//	log.Fatal(err)
			//}

			fmt.Printf("%s : %s \n", k, string(v))

		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

}
