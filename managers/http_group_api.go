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
	Mnemonic:	http_groups_api
	Abstract:	This provides the API interface for port groups (all URLs underneath /tegu/groups/).
				The main work functions (group_api_{get|put|post|delete}) all generate
				json formatted data to the output device (we assume back to the requesting
				browser/user-agent).

				These requests are supported:
					POST /tegu/group/
					PUT /tegu/group/{group_id}/
					GET /tegu/group/
					GET /tegu/group/{group_id}/
					DELETE /tegu/group/{group_id}/

	Author:		Robert Eby

	Mods:		04 Aug 2015 - Created.
*/

package managers

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

/*
 *  All requests to the /tegu/group/ URL subtree are funneled here for handling.
 */
func group_api_handler( out http.ResponseWriter, in *http.Request ) {
	code, msg, userid, tenant_id := api_pre_check(in)
	if code == http.StatusOK {
		data := dig_data( in )
		if data == nil {						// missing data -- punt early
			http_sheep.Baa( 1, "http: group_api_handler called without data: %s", in.Method )
			code = http.StatusBadRequest
			msg = "missing data"
		} else {
			http_sheep.Baa( 1, "Request from %s: %s %s", in.RemoteAddr, in.Method, in.RequestURI )
			switch in.Method {
				case "PUT":
					code, msg = group_api_put( in, tenant_id, data )

				case "POST":
					code, msg = group_api_post( in, tenant_id, data )

				case "DELETE":
					code, msg = group_api_delete( in, tenant_id, data )

				case "GET":
					code, msg = group_api_get( in, tenant_id, data )

				default:
					http_sheep.Baa( 1, "group_api_handler called for unrecognised method: %s", in.Method )
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
 *	Parse and react to a POST to /tegu/group/. We expect JSON describing the group request, to wit:
 *		{
 *		  "name": "alphagroup",
 *		  "description": "Middle boxes for VPN firewall",
 *		  "subnet_id": "a2197a4d-7b0b-47e1-8515-43b9f6f5c993",
 *		  "port_specs": [
 *		    "6c2ef984-3d5d-4af6-99bf-30de5f6cd425",
 *		    "192.168.2.5",
 *		    "tegu6",
 *		    "fa:16:3e:94:5c:ed",
 *		    "ed28fef1-ab71-4d34-8e4d-5bb03b0cb5ca  b6f35bac-0428-4fd3-94f6-62e7c5676deb"
 *		  ]
 *		}
 *
 *	If there are no errors, a JSON object will be returned fully describing the created group.
 */
func group_api_post( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	http_sheep.Baa( 5, "Request data: " + string(data))
	code = http.StatusBadRequest

	// 1. Unmarshall the JSON request, check for required fields
	req, err := gizmos.Mk_PortGroup(data)
	if err != nil {
		msg = "Bad JSON: " + err.Error()
		return
	}
	if req.Id != "" {
		msg = "Id field cannot be specified."
		return
	}
	if req.SubnetId == "" {
		msg = "subnet_id field is required."
		return
	}
	if !gizmos.IsUUID(req.SubnetId) {
		// TODO - if subnet is not a UUID, translate name to UUID
		msg = "subnet_id field must be a UUID; name is not (currently) supported."
		return
	}
	if req.PortSpecs == nil || len(req.PortSpecs) == 0 {
		msg = "port_specs array is required and must be non-empty."
		return
	}

	// These fields cannot be assigned by the user
	req.GenerateNewUUID()
	req.Tenant_id = tenant_id
	req.FixURL(getScheme(), in.Host)

	// Validate PortSpecs array, and initialize Ports array
	msg = getPorts( req, tenant_id )
	if msg != "" {
		return
	}

	gizmos.PortGroups[req.Id] = req

	// Request a chkpt now, but don't wait on it
	cpr := ipc.Mk_chmsg( )
	cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )

	code = http.StatusCreated
	msg  = req.To_json()
	return
}

func getPorts( req *gizmos.PortGroup, tenant_id string ) string {
	// Get array of all ports for tenant and subnet
	tports := GetTenantPorts(tenant_id)

	nports := len(req.PortSpecs)
	req.Ports = make( []gizmos.PortPair, nports )
	for i := range req.PortSpecs {
		portspecs := strings.Split(req.PortSpecs[i], " ")
		switch len(portspecs) {
		case 1:
			// One portspec, specifies both ingress and egress port
			uuid, err := mapPortSpecToUuid(tports, tenant_id, req.SubnetId, portspecs[0])
			if err != nil {
				return err.Error()
			}
			req.Ports[i].Ingress = uuid
			req.Ports[i].Egress  = uuid

		case 2:
			// Two portspecs, one for ingress, one for egress
			uuid1, err := mapPortSpecToUuid(tports, tenant_id, req.SubnetId, portspecs[0])
			if err != nil {
				return err.Error()
			}
			uuid2, err := mapPortSpecToUuid(tports, tenant_id, req.SubnetId, portspecs[1])
			if err != nil {
				return err.Error()
			}
			req.Ports[i].Ingress = uuid1
			req.Ports[i].Egress  = uuid2

		default:
			return "Invalid portspec, too many subfields: " + req.PortSpecs[i]
		}
	}
	return ""
}

/*
 *  Handle a PUT request to modify a group.  Can only modify the name, description, and port_specs.
 */
func group_api_put( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	group_id := getObjectId(in)
	if group_id == "" {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	group := gizmos.PortGroups[group_id]
	if group == nil {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	if tenant_id != group.Tenant_id {
		code = http.StatusUnauthorized
		msg = "Unauthorized."
		return
	}

	// Unmarshall the JSON request, check for required fields
	req, err := gizmos.Mk_PortGroup(data)
	if err != nil {
		code = http.StatusBadRequest
		msg = "Bad JSON: " + err.Error()
		return
	}
	if req.SubnetId != "" {
		code = http.StatusUnauthorized
		msg = "You may only modify the name, description, or port list of a group."
		return
	}
	req.SubnetId = group.SubnetId	// needed to validate ports
	if len(req.PortSpecs) > 0 {
		msg = getPorts( req, tenant_id )
		if msg != "" {
			code = http.StatusBadRequest
			return
		}
		group.Ports = req.Ports
	}
	if req.Name != "" {
		group.Name = req.Name
	}
	if req.Description != "" {
		group.Description = req.Description
	}

	// Notify any chains using this group, that the group has been modified
	for _, chain := range gizmos.Chains {
		if chain.IsGroupInChain( group_id ) {
			ChainModified(chain)
		}
	}

	// Request a chkpt now, but don't wait on it
	cpr := ipc.Mk_chmsg( )
	cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )

	code = http.StatusOK
	msg = "Updated."
	return
}

/*
 * Handle a GET /tegu/group/ or GET /tegu/group/{group_id}/ request.
 * The first form lists all groups owned by the tenant, the second form list details of one group.
 */
func group_api_get( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	group_id := getObjectId(in)
	if group_id == "" {
		// List all groups
		sep := "\n"
		bs := bytes.NewBufferString("{\n")
		bs.WriteString("  \"groups\": [")
		for _, g := range gizmos.PortGroups {
			if g != nil && tenant_id == g.Tenant_id {
				g.FixURL(getScheme(), in.Host)
				bs.WriteString(fmt.Sprintf("%s    {\n", sep))
				if g.Name != "" {
					bs.WriteString(fmt.Sprintf("      %q: %q,\n", "name", g.Name))
				}
				bs.WriteString(fmt.Sprintf("      %q: %q,\n", "id", g.Id))
				bs.WriteString(fmt.Sprintf("      %q: %q,\n", "tenant_id", g.Tenant_id))
				bs.WriteString(fmt.Sprintf("      %q: %q\n", "url", g.Url))
				bs.WriteString("    }")
				sep = ",\n"
			}
		}
		bs.WriteString("\n  ]\n")
		bs.WriteString("}\n")
		code = http.StatusOK
		msg = bs.String()
	} else {
		group := gizmos.PortGroups[group_id]
		if group == nil {
			code = http.StatusNotFound
			msg = "Not found."
			return
		}
		if tenant_id != group.Tenant_id {
			code = http.StatusUnauthorized
			msg = "Unauthorized."
			return
		}
		code = http.StatusOK
		group.FixURL(getScheme(), in.Host)
		msg  = group.To_json()
	}
	return
}

/*
 * Handle a DELETE /tegu/group/{group_id}/ request.
 */
func group_api_delete( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	group_id := getObjectId(in)
	group := gizmos.PortGroups[group_id]
	if group == nil {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	if tenant_id != group.Tenant_id {
		code = http.StatusUnauthorized
		msg = "Unauthorized."
		return
	}

	chain := gizmos.Find_chain_using_group(group_id)
	if chain != nil {
		code = http.StatusForbidden
		msg = "Cannot remove this group until the chain " + *chain + " is removed."
		return
	}

	delete(gizmos.PortGroups, group_id)

	// Request a chkpt now, but don't wait on it
	cpr := ipc.Mk_chmsg( )
	cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )

	code = http.StatusNoContent
	msg = ""
	return
}

func mapPortSpecToUuid( ports []*map[string]string, tenant_id string, subnet string, spec string ) ( string, error ) {
	spec   = strings.ToLower(spec)
	subnet = strings.ToLower(subnet)
	if gizmos.IsUUID(spec) {
		for k := range ports {
			v := *ports[k]
			if v["uuid"] == spec && v["subnet"] == subnet {
				return v["uuid"], nil
			}
		}
		return "", fmt.Errorf("Tenant "+tenant_id+" does not have a port on subnet "+subnet+" with UUID = "+spec)
	}
	if gizmos.IsMAC(spec) {
		for k := range ports {
			v := *ports[k]
			if v["mac"] == spec && v["subnet"] == subnet {
				return v["uuid"], nil
			}
		}
		return "", fmt.Errorf("Tenant "+tenant_id+" does not have a port on subnet "+subnet+" with MAC = "+spec)
	}
	if gizmos.IsIPv4(spec) || gizmos.IsIPv6(spec) {
		for k := range ports {
			v := *ports[k]
			if v["ip"] == spec && v["subnet"] == subnet {
				return v["uuid"], nil
			}
		}
		return "", fmt.Errorf("Tenant "+tenant_id+" does not have a port on subnet "+subnet+" with IP = "+spec)
	}
	if strings.Index(spec, "/") >= 0 {
// TODO if spec looks like a (Nova) VM name, in the form tenant/name,
// which will select an arbitrary port connected to both that VM and
// the subnet listed above (not recommended if a VM has multiple ports)
		return "", fmt.Errorf("Unimplemented port spec "+spec)
	}
	return "", fmt.Errorf("Unrecognized port spec "+spec)
}

// store a mac in a UUID as "00000000-0000-0000-0000-xxxxxxxxxxxx"
func mac2uuid(s string) string {
	bs := bytes.NewBufferString("00000000-0000-0000-0000-")
	parts := strings.Split(s, ":")
	for _, t:= range parts {
		if len(t) < 2 {
			bs.WriteString("0")
		}
		bs.WriteString(t)
	}
	return bs.String()
}

func getScheme() (string) {
	if (isSSL) {
		return "https"
	}
	return "http"
}

func getObjectId(in *http.Request) (string) {
	t := in.URL.Path
	tt := strings.Split(t, "/")
	if len(tt) > 4 {
		return tt[3]
	} else {
		return ""
	}
}

func api_pre_check(in *http.Request) (code int, msg string, userid string, projid string) {
	code = http.StatusOK	// response code to return
	msg  = ""				// data to go in response (assumed to be JSON, if code = StatusOK or StatusCreated)
	userid = "-"
	projid = ""
	if accept_requests  {
		if in.Header != nil && in.Header["X-Auth-Tegu"] != nil {
			auth := in.Header["X-Auth-Tegu"][0]
			uproj := token_has_osroles_with_UserProject( &auth, *admin_roles )
			if uproj != "" {	// if token has one of the roles listed in config file
				parts := strings.Split( uproj, "," )
				userid = parts[0]
				projid = parts[1]
			} else {
				code = http.StatusUnauthorized
				msg = "A valid token with a tegu_admin role is required to execute group commands"
			}
		} else {
			code = http.StatusUnauthorized
			msg = "The X-Auth-Tegu header must contain a valid token with an admin or tegu_admin role for this operation"
		}
	} else {
		code = http.StatusServiceUnavailable
		msg = "Tegu is running but not accepting requests; try again later"
	}
	return
}
