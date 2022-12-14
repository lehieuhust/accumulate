package walletd

import (
	"fmt"

	url2 "gitlab.com/accumulatenetwork/accumulate/pkg/url"
)

func ListADIs() (string, error) {
	b, err := GetWallet().GetBucket(BucketAdi)
	if err != nil {
		return "", err
	}

	var out string
	for _, v := range b.KeyValueList {
		u, err := url2.Parse(string(v.Key))
		if err != nil {
			out += fmt.Sprintf("%s\t:\t%x \n", v.Key, v.Value)
		} else {
			lab, err := FindLabelFromPubKey(v.Value)
			if err != nil {
				out += fmt.Sprintf("%v\t:\t%x \n", u, v.Value)
			} else {
				out += fmt.Sprintf("%v\t:\t%s\n", u, lab)
			}
		}
	}

	return out, nil
}

func getAdiList() (urls []url2.URL, err error) {
	b, err := GetWallet().GetBucket(BucketAdi)
	if err != nil {
		return nil, err
	}

	for _, v := range b.KeyValueList {
		u, err := url2.Parse(string(v.Key))
		if err != nil {
			return nil, err
		}
		urls = append(urls, *u)
	}
	return urls, nil
}
