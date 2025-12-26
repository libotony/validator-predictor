package test

import "os"

func getNodeURL() string {
	if url := os.Getenv("THOR_NODE"); url != "" {
		return url
	}
	return "https://mainnet.vechain.org"
}

var nodeURL = getNodeURL()
