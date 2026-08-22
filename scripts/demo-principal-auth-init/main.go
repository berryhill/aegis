package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/berryhill/aegis/internal/principalauth"
)

type input struct {
	PrincipalID string `json:"principal_id"`
	Password    string `json:"password"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: demo-principal-auth-init INPUT_FILE STATE_DIR")
		os.Exit(2)
	}
	file, err := os.Open(os.Args[1])
	if err != nil {
		fail(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var request input
	if err = decoder.Decode(&request); err != nil {
		fail(err)
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		fail(fmt.Errorf("input has trailing data"))
	}
	password := []byte(request.Password)
	defer func() {
		for index := range password {
			password[index] = 0
		}
	}()
	record, err := principalauth.Enroll(request.PrincipalID, password)
	if err != nil {
		fail(err)
	}
	if err = principalauth.Publish(filepath.Join(os.Args[2], "auth", principalauth.FileName), record); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "principal authentication fixture initialization failed")
	_ = err
	os.Exit(1)
}
