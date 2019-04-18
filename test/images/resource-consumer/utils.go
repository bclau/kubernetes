/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
)

var (
	consumeCPUBinary = getBinaryPath("./consume-cpu/consume-cpu")
	consumeMemBinary = getBinaryPath("./consume-memory/consume-memory")
)

func getBinaryPath(path string) string {
	if runtime.GOOS == "windows" {
		path, _ = filepath.Abs(fmt.Sprintf("%s.exe", path))
		return path
	}
	return path
}

// ConsumeCPU consumes a given number of millicores for the specified duration.
func ConsumeCPU(millicores int, durationSec int) {
	log.Printf("ConsumeCPU millicores: %v, durationSec: %v", millicores, durationSec)
	// creating new consume cpu process
	arg1 := fmt.Sprintf("-millicores=%d", millicores)
	arg2 := fmt.Sprintf("-duration-sec=%d", durationSec)
	consumeCPU := exec.Command(consumeCPUBinary, arg1, arg2)
	err := consumeCPU.Run()
	if err != nil {
		log.Printf("Error while consuming CPU: %v", err)
	}
}

// ConsumeMem consumes a given number of megabytes for the specified duration.
func ConsumeMem(megabytes int, durationSec int) {
	log.Printf("ConsumeMem megabytes: %v, durationSec: %v", megabytes, durationSec)
	megabytesString := strconv.Itoa(megabytes) + "M"
	durationSecString := strconv.Itoa(durationSec)
	// creating new consume memory process
	consumeMem := exec.Command(consumeMemBinary, "-m", "1", "--vm-bytes", megabytesString, "--vm-hang", "0", "-t", durationSecString)
	err := consumeMem.Run()
	if err != nil {
		log.Printf("Error while consuming memory: %v", err)
	}
}

// GetCurrentStatus prints out a no-op.
func GetCurrentStatus() {
	log.Printf("GetCurrentStatus")
	// not implemented
}
