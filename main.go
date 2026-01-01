package main

import (
	"github.com/nasnet-community/nasnet-panel-linux/cmd"
)

func main() {
	cmd.WebFS = WebFS
	cmd.Execute()
}
