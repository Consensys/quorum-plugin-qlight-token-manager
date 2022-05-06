## Prerequisites

* Go 1.16.x
* Make

## How-to

* Run `make` to create plugin distribution zip files for different OSes. 
* Copy the zip file in the desired OS folder to Quorum plugin folder.
* Define `qlight-token-manager` block in the `providers` section of plugin settings JSON
   ```
   "qlight-token-manager": {
      "name":"quorum-plugin-qlight-token-manager",
      "version":"1.0.0",
      "config": "file://<path-to>/qlight-token-manager-plugin-config.json"
   }
   ```

## qlight-token-manager-plugin-config.json

### Definition

```go
type config struct {
	URL, Method                      string
	TLSSkipVerify                    bool
	RefreshAnticipationInMillisecond int32
	Parameters                       map[string]string
}
```

### Example

```json
{
   "URL": "https://multi-tenancy-oauth2-server:4444/oauth2/token",
   "Method": "POST",
   "TLSSkipVerify": true,
   "RefreshAnticipationInMillisecond": 1000,
   "Parameters": {
      "grant_type":"client_credentials",
      "client_id":"${PSI}",
      "client_secret":"foofoo",
      "scope":"rpc://eth_* p2p://qlight rpc://admin_* rpc://personal_* rpc://quorumExtension_* rpc://rpc_modules psi://${PSI}?self.eoa=0x0&node.eoa=0x0",
      "audience":"Node1"
   }
}
```
