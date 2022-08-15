package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ConsenSys/quorum-qlight-token-manager-plugin-sdk-go/proto"
	"github.com/ConsenSys/quorum-qlight-token-manager-plugin-sdk-go/proto_common"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DefaultProtocolVersion = 1
)

var (
	DefaultHandshakeConfig = plugin.HandshakeConfig{
		ProtocolVersion:  DefaultProtocolVersion,
		MagicCookieKey:   "QUORUM_PLUGIN_MAGIC_COOKIE",
		MagicCookieValue: "CB9F51969613126D93468868990F77A8470EB9177503C5A38D437FEFF7786E0941152E05C06A9A3313391059132A7F9CED86C0783FE63A8B38F01623C8257664",
	}
)

// this is to demonstrate how to write a plugin that implements QLight token manager plugin interface
func main() {
	log.SetFlags(0) // don't display time
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: DefaultHandshakeConfig,
		Plugins: map[string]plugin.Plugin{
			"impl": &QlightTokenManagerPluginImpl{},
		},
		GRPCServer: plugin.DefaultGRPCServer,
	})
}

// implements 3 interfaces:
// 1. Initializer plugin interface - mandatory
// 2. QLight token manager plugin interface
// 3. GRPC Plugin from go-plugin
type QlightTokenManagerPluginImpl struct {
	proto_common.UnimplementedPluginInitializerServer
	proto.UnimplementedPluginQLightTokenRefresherServer
	plugin.Plugin
	cfg *config
}

var _ proto_common.PluginInitializerServer = &QlightTokenManagerPluginImpl{}
var _ proto.PluginQLightTokenRefresherServer = &QlightTokenManagerPluginImpl{}

func (h *QlightTokenManagerPluginImpl) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	proto_common.RegisterPluginInitializerServer(s, h)
	proto.RegisterPluginQLightTokenRefresherServer(s, h)
	return nil
}

func (h *QlightTokenManagerPluginImpl) GRPCClient(context.Context, *plugin.GRPCBroker, *grpc.ClientConn) (interface{}, error) {
	return nil, errors.New("not supported")
}

type config struct {
	URL, Method                      string
	TLSSkipVerify                    bool
	RefreshAnticipationInMillisecond int32
	Parameters                       map[string]string
}

func (c *config) validate() error {
	if c.URL == "" {
		return fmt.Errorf("url must be provided")
	}
	if c.Method == "" {
		return fmt.Errorf("method must be provided")
	}
	return nil
}

func (h *QlightTokenManagerPluginImpl) Init(_ context.Context, req *proto_common.PluginInitialization_Request) (*proto_common.PluginInitialization_Response, error) {
	var cfg config
	if err := json.Unmarshal(req.RawConfiguration, &cfg); err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid config: %s, err: %s", string(req.RawConfiguration), err.Error()))
	}
	if err := cfg.validate(); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	h.cfg = &cfg
	return &proto_common.PluginInitialization_Response{}, nil
}

type JWT struct {
	ExpireAt int64 `json:"exp"`
}

type OryResp struct {
	AccessToken      string `json:"access_token"`
	Error            string `json:"error,omitempty"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func (h *QlightTokenManagerPluginImpl) PluginQLightTokenManager(ctx context.Context, req *proto.PluginQLightTokenManager_Request) (*proto.PluginQLightTokenManager_Response, error) {
	anticipation := int32(0)
	if h.cfg.RefreshAnticipationInMillisecond > 0 {
		anticipation = h.cfg.RefreshAnticipationInMillisecond
	}
	return &proto.PluginQLightTokenManager_Response{RefreshAnticipationInMillisecond: anticipation}, nil
}

func (h *QlightTokenManagerPluginImpl) TokenRefresh(ctx context.Context, req *proto.TokenRefresh_Request) (*proto.TokenRefresh_Response, error) {
	log.Printf("refresh token %s\n", req.GetCurrentToken())
	token := req.GetCurrentToken()
	idx := strings.Index(token, " ")
	if idx >= 0 {
		token = token[idx+1:]
	}
	log.Printf("token=%s\n", token)
	split := strings.Split(token, ".")
	log.Printf("split=%v\n", split)

	data, _ := base64.RawStdEncoding.DecodeString(split[1]) // ignore error, we will refresh anyway in this case
	log.Printf("json=%s\n", string(data))

	jwt := &JWT{}
	json.Unmarshal(data, jwt) // ignore error, we will refresh anyway in this case

	log.Printf("expireAt=%v\n", jwt.ExpireAt)
	expireAt := time.Unix(jwt.ExpireAt, 0)
	log.Printf("expireAt=%v\n", expireAt)
	if time.Since(expireAt) < -time.Duration(h.cfg.RefreshAnticipationInMillisecond)*time.Millisecond {
		log.Println("return current token")
		return &proto.TokenRefresh_Response{Token: req.GetCurrentToken()}, nil
	}

	transCfg := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: h.cfg.TLSSkipVerify, // ignore expired SSL certificates
		},
	}
	client := &http.Client{Transport: transCfg}

	var body *bytes.Buffer
	var writer *multipart.Writer
	var encoded *url.Values
	if h.cfg.Method == "POST" || h.cfg.Method == "PUT" {
		body = &bytes.Buffer{}
		writer = multipart.NewWriter(body)
		for key, template := range h.cfg.Parameters {
			fw, err := writer.CreateFormField(key)
			if err != nil {
				return nil, err
			}
			_, err = io.Copy(fw, strings.NewReader(strings.Replace(template, "${PSI}", req.Psi, -1)))
			if err != nil {
				return nil, err
			}
		}

		err := writer.Close()
		if err != nil {
			return nil, err
		}
	} else if h.cfg.Method == "GET" {
		encoded = &url.Values{}
		for key, template := range h.cfg.Parameters {
			encoded.Set(key, strings.Replace(template, "${PSI}", req.Psi, -1))
		}
	} else { // JSON body encoding
		body = &bytes.Buffer{}
		m := make(map[string]string)
		for key, template := range h.cfg.Parameters {
			m[key] = strings.Replace(template, "${PSI}", req.Psi, -1)
		}
		err := json.NewEncoder(body).Encode(m)
		if err != nil {
			return nil, err
		}
	}

	var reader *bytes.Reader
	var url string = h.cfg.URL
	switch {
	case body != nil:
		reader = bytes.NewReader(body.Bytes())
	case encoded != nil:
		url += "?" + encoded.Encode()
	}

	request, err := http.NewRequestWithContext(ctx, h.cfg.Method, url, reader)
	if err != nil {
		return nil, err
	}
	if h.cfg.Method == "POST" || h.cfg.Method == "PUT" {
		request.Header.Set("Content-Type", writer.FormDataContentType())
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	data, err = ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	log.Printf("ory response=%s\n", string(data))

	oryResp := &OryResp{}
	err = json.Unmarshal(data, oryResp)
	if err != nil {
		return nil, err
	}
	if len(oryResp.Error) > 0 {
		return nil, fmt.Errorf("%s: %s", oryResp.Error, oryResp.ErrorDescription)
	}
	token = "bearer " + oryResp.AccessToken
	return &proto.TokenRefresh_Response{Token: token}, nil
}

// accessToken=$$(curl -k -s -X POST -F "grant_type=client_credentials" -F "client_id=$${PSI}" -F "client_secret=foofoo" -F "scope=rpc://eth_* p2p://qlight rpc://admin_* rpc://personal_* rpc://quorumExtension_* rpc://rpc_modules psi://$${PSI}?self.eoa=0x0&node.eoa=0x0" -F "audience=Node1" https://multi-tenancy-oauth2-server:4444/oauth2/token | jq '.access_token' -r)
