// vi: sw=4 ts=4:
/*
 ---------------------------------------------------------------------------
   Copyright (c) 2013-2015 AT&T Intellectual Property

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at:

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
 ---------------------------------------------------------------------------
*/


/*
	Mnemonic:	http_fc_api
	Abstract:	This provides the API interface for flow classifiers (all URLs underneath /tegu/fc/).
				The main work functions (fc_api_{get|put|post|delete}) all generate
				json formatted data to the output device (we assume back to the requesting
				browser/user-agent).

				These requests are supported:
					POST /tegu/fc/
					PUT /tegu/fc/{fc_id}/
					GET /tegu/fc/
					GET /tegu/fc/{fc_id}/
					DELETE /tegu/fc/{fc_id}/

	Author:		Robert Eby

	Mods:		04 Aug 2015 - Created.
*/

package managers

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"strings"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

/*
 *  All requests to the /tegu/fc/ URL subtree are funneled here for handling.
 */
func fc_api_handler( out http.ResponseWriter, in *http.Request ) {
	code, msg, userid, tenant_id := api_pre_check(in)
	if code == http.StatusOK {
		data := dig_data( in )
		if data == nil {						// missing data -- punt early
			http_sheep.Baa( 1, "http: fc_api_handler called without data: %s", in.Method )
			code = http.StatusBadRequest
			msg = "missing data"
		} else {
			http_sheep.Baa( 1, "Request from %s: %s %s", in.RemoteAddr, in.Method, in.RequestURI )
			switch in.Method {
				case "PUT":
					code, msg = fc_api_put( in, tenant_id, data )

				case "POST":
					code, msg = fc_api_post( in, tenant_id, data )

				case "DELETE":
					code, msg = fc_api_delete( in, tenant_id, data )

				case "GET":
					code, msg = fc_api_get( in, tenant_id, data )

				default:
					http_sheep.Baa( 1, "fc_api_handler called for unrecognised method: %s", in.Method )
					code = http.StatusMethodNotAllowed
					msg = fmt.Sprintf( "unrecognised method: %s", in.Method )
			}
		}
	}

	// Set response code and write response; set Content-type header for JSON
	hdr := out.Header()
	hdr.Add("Content-type", "application/json")
	if code != http.StatusOK && code != http.StatusCreated {
		http_sheep.Baa( 2, "Response: " + msg)
		msg = fmt.Sprintf(`{ "error": %q }`, msg)
	}
	out.WriteHeader(code)
	out.Write([]byte(msg))
	httplogger.LogRequest(in, userid, code, len(msg))
}

/*
 *	Parse and react to a POST to /tegu/fc/. We expect JSON describing the flow classifier request, to wit:
 *		{
 *		  "name": "steer_HTTP",
 *		  "description": "steer HTTP traffic to load balancer",
 *		  "protocol": 6,
 *		  "dest_port": 80,
 *		  "source_neutron_port": "6c2ef984-3d5d-4af6-99bf-30de5f6cd425"
 *		}
 *
 *	If there are no errors, a JSON object will be returned fully describing the created flow classifier.
 */
func fc_api_post( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	http_sheep.Baa( 5, "Request data: " + string(data))
	code = http.StatusBadRequest

	// 1. Unmarshall the JSON request, check for required fields
	req, err := gizmos.Mk_FlowClassifier(data)
	if err != nil {
		msg = "Bad JSON: " + err.Error()
		return
	}
	if req.Protocol == 0 {
		req.Protocol = 6	// default: 6=tcp
	}
	/*
	Source_port	int	 	 	`json:"source_port"`		// read only
	Dest_port	int	 	 	`json:"dest_port"`			// read only
	Source_ip	string	 	`json:"source_ip"`			// read only
	Dest_ip		string	 	`json:"dest_ip"`			// read only
	Source_Nport string	 	`json:"source_neutron_port"`// read only
	Dest_Nport	string 	 	`json:"dest_neutron_port"`	// read only

		if source_nport provided, then no source_ip/port
		if Dest_Nport provided, then no dest_ip/port
	*/
	if req.Source_ip == "" && req.Dest_ip == "" && req.Source_Nport == "" && req.Dest_Nport == "" {
		msg = "Must specify at least one source and/or destination port specification."
		return
	}
	if req.Source_port != 0 || req.Dest_port != 0 {
		if req.Source_port < 0 || req.Source_port > 0xFFFF {
			msg = "Source port must have a value between 0 and 65535"
			return
		}
		if req.Dest_port < 0 || req.Dest_port > 0xFFFF {
			msg = "Destination port must have a value between 0 and 65535"
			return
		}
		proto := req.Protocol
		if proto != 6 && proto != 17 && proto != 132 {
			msg = "Cannot specify a source or destination port when protocol is not 6 (TCP), 17 (UDP), or 132 (SCTP)"
			return
		}
	}
	if req.Source_ip != "" {
		err = validateIpAddress(req.Source_ip)
		if err != nil {
			msg = err.Error()
			return
		}
	}
	if req.Dest_ip != "" {
		err = validateIpAddress(req.Dest_ip)
		if err != nil {
			msg = err.Error()
			return
		}
	}
	if req.Source_ip != "" && req.Dest_ip != "" {
		sv4 := strings.Index(req.Source_ip, ":") < 0
		dv4 := strings.Index(req.Source_ip, ":") < 0
		if sv4 != dv4 {
			msg = "Both source_ip and dest_ip must be the same type (IPv4 or IPv6) of address."
			return
		}
	}

	// These fields cannot be assigned by the user
	req.GenerateNewUUID()
	req.Tenant_id = tenant_id
	req.FixURL(getScheme(), in.Host)

	// 3. Build and save group structure
	gizmos.Classifiers[req.Id] = req

	// Request a chkpt now, but don't wait on it
	cpr := ipc.Mk_chmsg( )
	cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )

	code = http.StatusCreated
	msg  = req.To_json()
	return
}

/*
 *  Handle a PUT request to modify a flow classifier.  Can only modify the name or description.
 */
func fc_api_put( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	fc_id := getObjectId(in)
	if fc_id == "" {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	fc := gizmos.Classifiers[fc_id]
	if fc == nil {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	if tenant_id != fc.Tenant_id {
		code = http.StatusUnauthorized
		msg = "Unauthorized."
		return
	}
	req, err := gizmos.Mk_FlowClassifier(data)
	if err != nil {
		code = http.StatusBadRequest
		msg = "Bad JSON: " + err.Error()
		return
	}
	// Check for attempt to modify other fields (which are all read only)
	// Go has no "&=" on booleans?  Really?  Sheesh!
	ok := (req.Protocol == 0) && (req.Source_port == 0) && (req.Dest_port == 0)
	ok = ok && (req.Source_ip == "") && (req.Dest_ip == "")
	ok = ok && (req.Source_Nport == "") && (req.Dest_Nport == "")
	if !ok {
		code = http.StatusBadRequest
		msg = "You may only modify the name or description of a flow classifier."
		return
	}
	if req.Name != "" {
		fc.Name = req.Name
	}
	if req.Description != "" {
		fc.Description = req.Description
	}

	// Request a chkpt now, but don't wait on it
	cpr := ipc.Mk_chmsg( )
	cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )

	code = http.StatusOK
	msg = "Updated."
	return
}

/*
 * Handle a GET /tegu/fc/ or GET /tegu/fc/{fc_id}/ request.
 * The first form lists all classifiers owned by the tenant, the second form list details of one FC.
 */
func fc_api_get( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	fc_id := getObjectId(in)
	if fc_id == "" {
		// List all classifiers owned by this tenant
		sep := "\n"
		bs := bytes.NewBufferString("{\n")
		bs.WriteString("  \"classifiers\": [")
		for _, f := range gizmos.Classifiers {
			if f != nil && tenant_id == f.Tenant_id {
				f.FixURL(getScheme(), in.Host)
				bs.WriteString(fmt.Sprintf("%s    {\n", sep))
				if f.Name != "" {
					bs.WriteString(fmt.Sprintf("      %q: %q,\n", "name", f.Name))
				}
				bs.WriteString(fmt.Sprintf("      %q: %q,\n", "id", f.Id))
				bs.WriteString(fmt.Sprintf("      %q: %q,\n", "tenant_id", f.Tenant_id))
				bs.WriteString(fmt.Sprintf("      %q: %q\n", "url", f.Url))
				bs.WriteString("    }")
				sep = ",\n"
			}
		}
		bs.WriteString("\n  ]\n")
		bs.WriteString("}\n")
		code = http.StatusOK
		msg = bs.String()
	} else {
		fc := gizmos.Classifiers[fc_id]
		if fc == nil {
			code = http.StatusNotFound
			msg = "Not found."
			return
		}
		if tenant_id != fc.Tenant_id {
			code = http.StatusUnauthorized
			msg = "Unauthorized."
			return
		}
		code = http.StatusOK
		fc.FixURL(getScheme(), in.Host)
		msg  = fc.To_json()
	}
	return
}

/*
 * Handle a DELETE /tegu/fc/{fc_id}/ request.
 */
func fc_api_delete( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	fc_id := getObjectId(in)
	fc := gizmos.Classifiers[fc_id]
	if fc == nil {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	if tenant_id != fc.Tenant_id {
		code = http.StatusUnauthorized
		msg = "Unauthorized."
		return
	}
	chain := gizmos.Find_chain_using_fc(fc_id)
	if chain != nil {
		code = http.StatusForbidden
		msg = "Cannot remove this flow classifier until the chain " + *chain + " is removed."
		return
	}

	delete(gizmos.Classifiers, fc_id)

	// Request a chkpt now, but don't wait on it
	cpr := ipc.Mk_chmsg( )
	cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )

	code = http.StatusNoContent
	msg = ""
	return
}

func validateIpAddress( s string ) ( error ) {
	ip := net.ParseIP( s )
	if ip == nil {
		return fmt.Errorf( "%s is not a valid IP address", s )
	}
	return nil
}
