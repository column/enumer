package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypeScript(t *testing.T) {
	cases := []struct {
		input      string
		hasComment bool
		output     string
		typeName   string
	}{
		{
			input:      "input_comment.go",
			hasComment: true,
			output:     "input_comment.ts",
			typeName:   "Owner",
		},
		{
			input:      "input_nocomment.go",
			hasComment: false,
			output:     "input_nocomment.ts",
			typeName:   "Gender",
		},
	}
	dir, err := os.Getwd()
	require.NoError(t, err)

	for _, testcase := range cases {
		var g Generator
		fullPath := filepath.Join(dir, "tsdata", testcase.input)
		g.parsePackage([]string{fullPath})
		g.generate(testcase.typeName, true, true, true, true, "noop", "", testcase.hasComment)

		output := filepath.Join(dir, "tsdata", testcase.output)
		expected, err := ioutil.ReadFile(output)
		require.NoError(t, err)
		assert.Equal(t, expected, g.tsBuf.Bytes())
	}
}
