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
	Mnemonic:	http_chain_api
	Abstract:	This provides the API interface for flow classifiers (all URLs underneath /tegu/chain/).
				The main work functions (chain_api_{get|put|post|delete}) all generate
				json formatted data to the output device (we assume back to the requesting
				browser/user-agent).

				These requests are supported:
					POST /tegu/chain/
					PUT /tegu/chain/{chain_id}/
					GET /tegu/chain/
					GET /tegu/chain/{chain_id}/
					DELETE /tegu/chain/{chain_id}/

	Author:		Robert Eby

	Mods:		04 Aug 2015 - Created.
*/

package managers

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

/*
 *  All requests to the /tegu/chain/ URL subtree are funneled here for handling.
 */
func chain_api_handler( out http.ResponseWriter, in *http.Request ) {
	code, msg, userid, tenant_id := api_pre_check(in)
	if code == http.StatusOK {
		data := dig_data( in )
		if data == nil {						// missing data -- punt early
			http_sheep.Baa( 1, "http: chain_api_handler called without data: %s", in.Method )
			code = http.StatusBadRequest
			msg = "missing data"
		} else {
			http_sheep.Baa( 1, "Request from %s: %s %s", in.RemoteAddr, in.Method, in.RequestURI )
			switch in.Method {
				case "PUT":
					code, msg = chain_api_put( in, tenant_id, data )

				case "POST":
					code, msg = chain_api_post( in, tenant_id, data )

				case "DELETE":
					code, msg = chain_api_delete( in, tenant_id, data )

				case "GET":
					code, msg = chain_api_get( in, tenant_id, data )

				default:
					http_sheep.Baa( 1, "chain_api_handler called for unrecognised method: %s", in.Method )
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
 *	Parse and react to a POST to /tegu/chain/. We expect JSON describing the flow classifier request, to wit:
 *		{
 *		  "name": "steer_HTTP",
 *		  "description": "steer HTTP traffic to load balancer",
 *		  "protocol": 6,
 *		  "dest_port": 80,
 *		  "source_neutron_port": "6c2ef984-3d5d-4af6-99bf-30de5f6cd425"
 *		}
 *
 *	If there are no errors, a JSON object will be returned fully describing the created chain.
 */
func chain_api_post( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	http_sheep.Baa( 5, "Request data: " + string(data))
	code = http.StatusOK

	// 1. Unmarshall the JSON request, check for required fields
	req, err := gizmos.Mk_Chain(data)
	if err != nil {
		code = http.StatusBadRequest
		msg = "Bad JSON: " + err.Error()
		return
	}

	// Check start/end times
	var startt int64
	var endt int64
	now    := time.Now().Unix()
	if req.Start_time == "" {
		startt = now
	} else {
		startt, err = strconv.ParseInt(req.Start_time, 0, 64)
	}
	if err != nil {
		code = http.StatusBadRequest
		msg = "Cannot parse start_time: " + err.Error()
		return
	}
	if req.End_time == "" {
		endt = startt
	} else if req.End_time[0:1] == "+" {
		endt, err = strconv.ParseInt(req.End_time[1:], 0, 64)
		endt += startt
	} else if req.End_time == "unbounded" {
		endt = gizmos.DEF_END_TS		// 1/1/2025
	} else {
		endt, err = strconv.ParseInt(req.End_time, 0, 64)
	}
	if err != nil {
		code = http.StatusBadRequest
		msg = "Cannot parse end_time: " + err.Error()
		return
	}
	if startt < now {
		startt = now
	}
	if endt <= startt {
		// If the user did not specify either a start or end time, then
		// the chain is created in a dormant state.  It must be modified
		// (with PUT) before it will become active.
		if req.Start_time != "" || req.End_time != "" {
			code = http.StatusBadRequest
			msg = fmt.Sprintf( "end_time (%d) <= start_time, (%d)", endt, startt )
			return
		}
	}
	req.SetWindow(startt, endt)

	// Validate fields
	if req.Classifiers == nil || len(req.Classifiers) == 0 {
		code = http.StatusBadRequest
		msg = "At least one Flow classifier is required."
		return
	}

	// Check Flow Classifiers
	fcarray := make( []*gizmos.FlowClassifier, 0)
	for _, v := range req.Classifiers {
		fc := gizmos.Classifiers[v]
		if fc == nil {
			code = http.StatusNotFound
			msg = "Flow classifier " + v + " does not exist."
			return
		}
		fcarray = append(fcarray, fc)
	}
	_, err = gizmos.MergeClassifiers(fcarray)	// Make sure these can be combined
	if err != nil {
		code = http.StatusBadRequest
		msg = err.Error()
		return
	}
	fcarray = nil

	// Check port groups
	if req.Groups == nil || len(req.Groups) == 0 {
		code = http.StatusBadRequest
		msg = "At least one Port group is required."
		return
	}
	for _, v := range req.Groups {
		if gizmos.PortGroups[v] == nil {
			code = http.StatusNotFound
			msg = "Group " + v + " does not exist."
			return
		}
	}
	for _, v1 := range req.Groups {
		for _, v2 := range req.Groups {
			if v1 != v2 {
				// Check for disallowed intersections between groups
				p1 := gizmos.PortGroups[v1]
				p2 := gizmos.PortGroups[v2]
				if p1.Intersects(p2) {
					code = http.StatusBadRequest
					msg = "Overlapping port groups are not allowed: "+v1+" and "+v2
					return
				}
			}
		}
	}

	// Check spatial and temporal overlap with other chains
	err = checkSpatialTemporalOverlap(req)
	if err != nil {
		code = http.StatusConflict
		msg = err.Error()
		return
	}

	// These fields cannot be assigned by the user
	req.GenerateNewUUID()
	req.Tenant_id = tenant_id
	req.FixURL(getScheme(), in.Host)

	// Send Chain pledge to res mgr
	request := ipc.Mk_chmsg( )
	my_ch := make( chan *ipc.Chmsg )					// allocate channel for responses to our requests
	defer close( my_ch )								// close it on return
	ip := gizmos.Pledge( req )							// must pass an interface pointer to resmgr
	request.Send_req( rmgr_ch, my_ch, REQ_ADD, &ip, nil )	// network OK'd it, so add it to the inventory
	request = <- my_ch										// wait for completion

	if request.State == nil {
		// Request a chkpt now, but don't wait on it
		cpr := ipc.Mk_chmsg( )
		cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )
	} else {
		err = fmt.Errorf( "%s", request.State )
	}

	// This goes against everything I believe, but I'm tired of asking the res mgr for things!
	gizmos.Chains[req.Id] = req

	// Ask sfs_mgr to build a plan for this new chain
	ch := ipc.Mk_chmsg( )
	ch.Send_req( sfs_ch, nil, REQ_GENPLAN, req, nil )

	code = http.StatusCreated
	msg  = req.To_json()
	return
}

/*
 * Handle a PUT request to modify a chain. You can modify the name, description,
 * start/end times only for now (maybe add groups later).
 */
func chain_api_put( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	chain_id := getObjectId(in)
	if chain_id == "" {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	chain := gizmos.Chains[chain_id]
	if chain == nil {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	if tenant_id != chain.Tenant_id {
		code = http.StatusUnauthorized
		msg = "Unauthorized."
		return
	}

	// Unmarshall the JSON request, check for required fields
	req, err := gizmos.Mk_Chain(data)
	if err != nil {
		code = http.StatusBadRequest
		msg = "Bad JSON: " + err.Error()
		return
	}
	if req.Name != "" {
		chain.Name = req.Name
	}
	if req.Description != "" {
		chain.Description = req.Description
	}
	notif_resmgr := false
	if req.Start_time != "" || req.End_time != "" {
		// Changing the start or end time -- this has the potential of
		// causing the chain to become active/inactive
		wasactive := chain.Is_active()
		startt, endt := chain.GetWindow()
		if req.Start_time != "" && req.End_time != "" {
			// Change both times
			startt, err = strconv.ParseInt(req.Start_time, 0, 64)
			if err != nil {
				code = http.StatusBadRequest
				msg = "Cannot parse start_time: " + err.Error()
				return
			}
			if req.End_time[0:1] == "+" {
				endt, err = strconv.ParseInt(req.End_time[1:], 0, 64)
				endt += startt
			} else if req.End_time == "unbounded" {
				endt = gizmos.DEF_END_TS		// 1/1/2025
			} else {
				endt, err = strconv.ParseInt(req.End_time, 0, 64)
			}
			if err != nil {
				code = http.StatusBadRequest
				msg = "Cannot parse end_time: " + err.Error()
				return
			}
			now := time.Now().Unix()
			if startt < now {
				startt = now
			}
			if endt <= startt {
				code = http.StatusBadRequest
				msg = fmt.Sprintf( "end_time (%d) <= start_time, (%d)", endt, startt )
				return
			}
		} else if req.Start_time != "" {
			// Change just the start time
			startt, err = strconv.ParseInt(req.Start_time, 0, 64)
			if err != nil {
				code = http.StatusBadRequest
				msg = "Cannot parse start_time: " + err.Error()
				return
			}
			now := time.Now().Unix()
			if startt < now {
				startt = now
			}
			if endt <= startt {
				code = http.StatusBadRequest
				msg = fmt.Sprintf( "end_time (%d) <= start_time, (%d)", endt, startt )
				return
			}
		} else {
			// Change just the end time
			if req.End_time[0:1] == "+" {
				endt, err = strconv.ParseInt(req.End_time[1:], 0, 64)
				endt += startt
			} else if req.End_time == "unbounded" {
				endt = gizmos.DEF_END_TS		// 1/1/2025
			} else {
				endt, err = strconv.ParseInt(req.End_time, 0, 64)
			}
			if err != nil {
				code = http.StatusBadRequest
				msg = "Cannot parse end_time: " + err.Error()
				return
			}
			if endt <= startt {
				code = http.StatusBadRequest
				msg = fmt.Sprintf( "end_time (%d) <= start_time, (%d)", endt, startt )
				return
			}
		}
		// TODO - before assigning times, check for checkSpatialTemporalOverlap() with new times
		chain.SetWindow(startt, endt)
		isactive := chain.Is_active()
		notif_resmgr = (wasactive != isactive)
		if wasactive && !isactive {
			// Chain is no longer active, this forces an "undo"
			chain.Set_pushed()
		}
	}

	// TODO handle other fields?? e.g. modify groups?

	// Request a chkpt now, but don't wait on it
	cpr := ipc.Mk_chmsg( )
	cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )

	// Notify resmgr to push reservations now if pledge changes state
	if notif_resmgr {
		cpr.Send_req( rmgr_ch, nil, REQ_PUSHNOW, nil, nil )
	}

	code = http.StatusOK
	msg = "Updated."
	return
}

/*
 * Handle a GET /tegu/chain/ or GET /tegu/chain/{chain_id}/ request.
 * The first form lists all chains owned by the tenant, the second form list details of one chain.
 */
func chain_api_get( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	chain_id := getObjectId(in)
	if chain_id == "" {
		// List all chains
		sep := "\n"
		bs := bytes.NewBufferString("{\n")
		bs.WriteString("  \"chains\": [")
		for _, f := range gizmos.Chains {
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
		chain := gizmos.Chains[chain_id]
		if chain == nil {
			code = http.StatusNotFound
			msg = "Not found."
			return
		}
		if tenant_id != chain.Tenant_id {
			code = http.StatusUnauthorized
			msg = "Unauthorized."
			return
		}
		code = http.StatusOK
		chain.FixURL(getScheme(), in.Host)
		msg  = chain.To_json()
	}
	return
}

/*
 * Handle a DELETE /tegu/chain/{chain_id}/ request.
 */
func chain_api_delete( in *http.Request, tenant_id string, data []byte ) (code int, msg string) {
	chain_id := getObjectId(in)
	chain := gizmos.Chains[chain_id]
	if chain == nil {
		code = http.StatusNotFound
		msg = "Not found."
		return
	}
	if tenant_id != chain.Tenant_id {
		code = http.StatusUnauthorized
		msg = "Unauthorized."
		return
	}
	if chain.Is_active() {
		code = http.StatusConflict
		msg = "Chain is active; cannot be deleted.  Use PUT to make the chain inactive first."
		return
	}

	// Remove from reservation mgr
	req := ipc.Mk_chmsg( )
	my_ch := make( chan *ipc.Chmsg )					// allocate channel for responses to our requests
	defer close( my_ch )								// close it on return
	namepluscookie := []*string { &chain_id, &empty_str }
	req.Send_req( rmgr_ch, my_ch, REQ_DEL, namepluscookie, nil )	// remove the reservation
	req = <- my_ch										// wait for completion

	if req.State == nil {
		// Request a chkpt now, but don't wait on it
		cpr := ipc.Mk_chmsg( )
		cpr.Send_req( rmgr_ch, nil, REQ_CHKPT, nil, nil )
	}

	// Remove local copy
	delete(gizmos.Chains, chain_id)

	code = http.StatusNoContent
	msg = ""
	return
}

/*
	Check if the chain "p" overlaps with any of the existing chains, as the result of an overlap in time
	(either partially or completely) and conflicting ports and/or classifiers.
 */
func checkSpatialTemporalOverlap(p *gizmos.Chain) error {
	for _, c := range gizmos.Chains {
		if p.IntersectsTemporaly(c) {
//      	TODO - if other conflicts -> error
		}
	}
	return nil
}

/*
	Call this function if a chain may be active and needs to be re-pushed.
	e.g. if the groups in the chain have changed.
 */
func ChainModified(p *gizmos.Chain) {
	if p.Is_active() {
		p.Reset_pushed()
	}
	cpr := ipc.Mk_chmsg( )
	cpr.Send_req( sfs_ch, nil, REQ_GENPLAN, p, nil )	// this will push when the plan is re-gened
}
