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
	Mnemonic:	fq_mgr_pass
	Abstract:	flow/queue manager passthrough reservation related things.

	Date:		26 January 2016
	Author:		E. Scott Daniels

	Mods:
*/

package managers

import (
	"encoding/json"

	"github.com/att/gopkgs/ipc"
)


/*
	Send a passthrough flowmod generation request to the agent manager.
	This is basically taking the struct that the reservation manager 
	filled in and converting it to a map.

	Send a bandwidth endpoint flow-mod request to the agent manager.
	This is little more than a wrapper that converts the fq_req into
	an agent request. The ultimate agent action is to put in all
	needed flow-mods on an endpoint host in one go, so no need for
	individual requests for each and no need for tegu to understand
	the acutal flow-mod mechanics any more.

	Yes, this probably _could_ be pushed up into the reservation manager
	and sent from there to the agent manager, but for now, since the
	ip2mac information is local to fq-mgr, we'll keep it here.  (That
	info is local to fq-mgr b/c in the original Tegu it came straight
	in from skoogi and it was fq-mgr's job to interface with skoogi.)
*/
func send_pt_fmods( data *Fq_req, ip2mac map[string]*string, phost_suffix *string ) {


	if *data.Swid == "" {									// we must have a switch name to set bandwidth fmods
		fq_sheep.Baa( 1, "unable to send passthrough fmod request to agent: no switch defined in input data" )
		return
	}

	host := data.Swid
	if phost_suffix != nil {									// we need to add the physical host suffix
		host = add_phost_suffix( host, phost_suffix )
	}

	if data.Match.Smac != nil {									// caller can pass in IP and we'll convert it
		if ip2mac[*data.Match.Smac] != nil {
			data.Match.Smac = ip2mac[*data.Match.Smac]			// res-mgr thinks in IP, flow-mods need mac; convert
		}
	}

	msg := &agent_cmd{ Ctype: "action_list" }					// create a message for agent manager to send to an agent
	msg.Actions = make( []action, 1 )							// just a single action
	msg.Actions[0].Atype = "passthru"							// set all related passthrough flow-mods
	msg.Actions[0].Hosts = make( []string, 1 )					// passthrough flow-mods created on just one host
	msg.Actions[0].Hosts[0] = *host
	msg.Actions[0].Data = data.To_pt_map()						// convert useful data from caller into parms for agent

	json, err := json.Marshal( msg )							// bundle into a json string
	if err != nil {
		fq_sheep.Baa( 0, "unable to build json to set passthrough flow-mods" )
	} else {
		tmsg := ipc.Mk_chmsg( )
		tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, string( json ), nil )		// send as a short request to one agent
	}

	fq_sheep.Baa( 2, "passthru flow-mod request sent to agent manager: %s", json )
}
