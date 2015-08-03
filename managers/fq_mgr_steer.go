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

	Mnemonic:	fq_mgr_steering
	Abstract:	flow/queue manager functions that are directly related to steering
				(broken out of fq_mgr to make merging easier).

	Date:		03 Nov 2014
	Author:		E. Scott Daniels

	Mods:		27 Feb 2015 - changes to deal with lazy update and to correct l* bug.
				15 Jun 2015 - Cleaned up commented out lines a bit.
*/

package managers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/att/gopkgs/ipc"
)


/*
	Not to be confused with send_meta_fmods in res_mgr. This needs to be extended
	such that resmgr can just send fq-mgr a request to invoke this.

*/
func send_meta_fm( hlist []string, table int, cookie int, pattern string ) {
	tmsg := ipc.Mk_chmsg( )

	msg := &agent_cmd{ Ctype: "action_list" }				// create an agent message
	msg.Actions = make( []action, 1 )
	msg.Actions[0].Atype = "flowmod"
	msg.Actions[0].Hosts = hlist
	msg.Actions[0].Fdata = make( []string, 1 )
	msg.Actions[0].Fdata[0] = fmt.Sprintf( `-T %d -I -t 0 --match --action -m %s -N add 0x%x br-int`, table, pattern, cookie )

	json, err := json.Marshal( msg )										// bundle into a json string
	if err != nil {
		fq_sheep.Baa( 0, "steer: unable to build json to set meta flow mod" )
	} else {
		fq_sheep.Baa( 2, "meta json: %s", json )
		tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, string( json ), nil )		// send as a short request to one agent
	}
}


/*
	Send flow-mod(s) to the agent for steering.
	The fq_req contains data that are neither match or action oriented (priority, expiry, etc) are or
	macht or action only (late binding mac value), and a set of match and action paramters that are
	applied depending on where they are found.
	Data expected in the fq_req:
		Nxt_mac - the mac address that is to be set on the action as dest
		Expiry  - the timeout for the fmod(s)
		Ip1/2	- The src/dest IP addresses for match (one must be supplied)
		Meta	- The meta value to set/match (both optional)
		Swid	- The switch DPID or host name (ovs) (used as -h option)
		Swport	- The switch port to match (inbound)
		Table	- Table number to put the flow mod into
		Rsub    - A list (space separated) of table numbers to resub to in the order listed.
		Lbmac	- Assumed to be the mac address associated with the switch port when
					switch port is -128. This is passed on the -i option to the
					agent allowing the underlying interface to do late binding
					of the port based on the mac address of the mbox.
		Pri		- Fmod priority

	TODO: this needs to be expanded to be generic and handle all possible match/action parms
			not just the ones that are specific to steering.  It will probably need an on-all
			flag in the main request struct rather than deducing it from parms.
*/
func send_stfmod_agent( data *Fq_req, ip2mac map[string]*string, hlist *string ) {
	var hosts []string			// hosts that the fmod will target

	if data.Pri <= 0 {
		data.Pri = 100
	}


	table := ""
	if data.Table > 0 {
		table = fmt.Sprintf( "-T %d ", data.Table )
	}
	/*
	//===== right now no restriction/checking on some kind of source/dest
	else {														// for table 0 we insist on having at least a src IP or port or a dest ip
		if data.Match.Ip1 == nil && data.Match.Ip2 == nil {
			if data.Match.Swport == -1 {
				fq_sheep.Baa( 0, "ERR: cannot set steering fmod: both source and dest IP addresses nil and no inbound switch port set" )
				return
			}
		}
	}
	*/

	match_opts := "--match"					// build match options

	if data.Match.Meta != nil {
		if *data.Match.Meta != "" {
			match_opts += " -m " + *data.Match.Meta		// allow caller to override if they know better
		}
	}

	//on_all := data.Swid == nil 							// if no switch id, then we write to all
	if data.Swid == nil {									// no switch id, then write to all hosts
		hosts = strings.Split( *hlist, " " )
	} else {
		hosts = strings.Split( *data.Swid, " " )	
	}

	fq_sheep.Baa( 2, "sending steering metadata flow-mods to %d hosts alt-table base %d", len( hosts ), 90 )
	send_meta_fm( hosts,  90, 0xe5d, "0x01/0x01" )			// TODO: these need to use the same base that res-mgr is using
	send_meta_fm( hosts,  91, 0xe5d, "0x02/0x02" )

	if data.Match.Swport >= 0  {						// valid port
		match_opts += fmt.Sprintf( " -i %d", data.Match.Swport )
	} else {
		if data.Match.Swport == -128 {				// late binding port, we sub in the late binding MAC that was given
			if data.Lbmac != nil {
				match_opts += fmt.Sprintf( " -i %s", *data.Lbmac )
			} else {
				fq_sheep.Baa( 1, "ERR: cannot set steering fmod: late binding port supplied, but late binding MAC was nil" )
			}
		}
	}

	smac := data.Match.Smac								// smac wins if both smac and sip are given
	if smac == nil {
		if data.Match.Ip1 != nil {						// smac missing, set src IP (needed to support multiple res)
			toks := strings.Split( *data.Match.Ip1, "/" )	// split off project/
			match_opts += " -S " + toks[len( toks )-1]
		}
/*
		if data.Match.Ip1 != nil {						// src supplied, match on src
			smac = ip2mac[*data.Match.Ip1]
			if smac == nil {
				fq_sheep.Baa( 0, "ERR: cannot set steering fmod: src IP did not translate to MAC: %s", *data.Match.Ip1 )
				return
			}
		}
*/
	}
	if smac != nil {
		match_opts += " -s " + *smac
	}

	dmac := data.Match.Dmac								// dmac wins if both dmac and sip are given
	if dmac == nil {
		if data.Match.Ip2 != nil {						// src supplied, match on src
			dmac = ip2mac[*data.Match.Ip2]
			if dmac == nil {
				fq_sheep.Baa( 0, "ERR: cannot set steering fmod: dst IP did not translate to MAC: %s", *data.Match.Ip2 )
				return
			}
		}
	}
	if dmac != nil {
		match_opts += " -d " + *dmac
	}

	if *data.Match.Tpsport >= "0" && data.Protocol != nil {						// we allow 0 as that means match all of this protocol
        match_opts += fmt.Sprintf( " -p %s:%s", *data.Protocol, *data.Match.Tpsport )
    }

    if *data.Match.Tpdport >= "0" && data.Protocol != nil {
        match_opts += fmt.Sprintf( " -P %s:%s", *data.Protocol, *data.Match.Tpdport )
    }

	action_opts := ""

	if data.Action.Dmac != nil {						
		action_opts += " -d " + *data.Action.Dmac
	}
	if data.Action.Smac != nil {
		action_opts += " -s " + *data.Action.Smac
	}

	if data.Nxt_mac != nil {
		action_opts += " -d " + *data.Nxt_mac			// add next hop if supplied -- last mbox won't have a next hop, but needs to exist to skip p100 fmod
	}

	if data.Action.Meta != nil {						// CAUTION: ovs barfs on the command if write metadata isn't last
		if *data.Action.Meta != "" {
			action_opts += " -m " + *data.Action.Meta
		}
	}

	if data.Action.Resub != nil { 						// action options order may be sensitive; ensure -R is last
		toks := strings.Split( *data.Action.Resub, " " )
		for i := range toks {
			action_opts += " -R ," + toks[i]
		}
	}

	output := "-N"			// TODO: allow output action to be passed in

	//action_opts = fmt.Sprintf( "--action %s -R ,0 -N", action_opts )		// set up actions; may be order sensitive so -R and -N LAST
	action_opts = fmt.Sprintf( "--action %s %s", action_opts, output )		// set up actions

	//base_json := `{ "ctype": "action_list", "actions": [ { "atype": "flowmod", "fdata": [ `

	tmsg := ipc.Mk_chmsg( )

	msg := &agent_cmd{ Ctype: "action_list" }				// create an agent message
	msg.Actions = make( []action, 1 )
	msg.Actions[0].Atype = "flowmod"
	msg.Actions[0].Hosts = make( []string, 1 )
	msg.Actions[0].Hosts = hosts
	msg.Actions[0].Fdata = make( []string, 1 )
	msg.Actions[0].Fdata[0] = fmt.Sprintf( `%s -t %d -p %d %s %s add 0xedde br-int`, table, data.Expiry, data.Pri, match_opts, action_opts )

	json, err := json.Marshal( msg )			// bundle into a json string
	if err != nil {
		fq_sheep.Baa( 0, "steer: unable to build json to set flow mod" )
	} else {
		fq_sheep.Baa( 2, "stfmod json: %s", json )
		tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, string( json ), nil )		// send as a short request to one agent
	}
}
