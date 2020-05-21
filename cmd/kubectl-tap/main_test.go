// Copyright 2020 Soluble Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"bytes"
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/require"
)

type MockExiter struct {
	Code int
}

func (m *MockExiter) Exit(code int) {
	m.Code = code
}

func Test_RootCmd(t *testing.T) {
	require := require.New(t)
	exiter := &MockExiter{}
	cmd := NewRootCmd(exiter)
	b := bytes.NewBufferString("")
	cmd.SetOutput(b)
	err := cmd.Execute()
	require.Nil(err)
	out, err := ioutil.ReadAll(b)
	require.Nil(err)
	ub := bytes.NewBufferString("")
	cmd.SetOutput(ub)
	err = cmd.Usage()
	require.Nil(err)
	usage, err := ioutil.ReadAll(ub)
	require.Nil(err)
	require.Equal(usage, out)
	require.Equal(64, exiter.Code, "rootCmd should always call os.Exit(64)")
}

func Test_VersionCmd(t *testing.T) {
	require := require.New(t)
	b := bytes.NewBufferString("")
	cmd := NewVersionCmd()
	cmd.SetOutput(b)
	err := cmd.Execute()
	require.Nil(err)
	out, err := ioutil.ReadAll(b)
	require.Nil(err)
	require.Contains(string(out), "commit: ", "versionCmd does not produce expected output")
}
