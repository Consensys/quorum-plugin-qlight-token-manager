package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJSON(t *testing.T) {
	cfg := config{}
	err := json.Unmarshal([]byte(`{
		"tlsskipverify":true,
		"url":"url",
		"method":"method",
		"refreshanticipationinmillisecond":1001,
		"parameters":{"test":"test"}
	}`), &cfg)
	assert.NoError(t, err)
	assert.True(t, cfg.TLSSkipVerify)
	assert.Equal(t, "url", cfg.URL)
	assert.Equal(t, "method", cfg.Method)
	assert.Equal(t, int32(1001), cfg.RefreshAnticipationInMillisecond)
	assert.Equal(t, map[string]string{
		"test": "test",
	}, cfg.Parameters)
}
