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

	Mnemonic:	fq_mgr
	Abstract:	flow/queue manager. This module contains the goroutine that
				listens on the fq_ch for messages that cause flow mods to be
				sent (skoogi reservations) and OVS queue commands to be generated.

	Config:		These variables are referenced if in the config file (defaults in parens):
					fqmgr:ssq_cmd     - the command to execute when needing to adjust switch queues  (/opt/app/set_switch_queues)
					fqmgr:queue_check - the frequency (seconds) between checks to see if queues need to be reset (5)
					fqmgr:host_check  - the frequency (seconds) between checks to see  what _real_ hosts open stack reports (180)
					fqmgr:switch_hosts- A space sep list of hosts to set switch queues on; if given then openstack is _not_ queried (no list)
					default:sdn_host  - the host name where skoogi (sdn controller) is running
					
	Date:		29 December 2013
	Author:		E. Scott Daniels

	Mods:		30 Apr 2014 (sd) - Changes to support pushing flow-mods and reservations to an agent. Tegu-lite
				05 May 2014 (sd) - Now sends the host list to the agent manager in addition to keeping a copy
					for it's personal use.
				12 May 2014 (sd) - Reverts dscp values to 'original' at the egress switch
				19 May 2014 (sd) - Changes to allow floating ip to be supplied as a part of the flow mod.
				07 Jul 2014 - Added support for reservation refresh.
				20 Aug 2014 - Corrected shifting of mdscp value in the match portion of the flowmod (it wasn't being shifted) (bug 210)
				25 Aug 2014 - Major rewrite to send_fmod_agent; now uses the fq_req struct to make it more
					generic and flexible.
				27 Aug 2014 - Small fixes during testing.
				03 Sep 2014 - Correct bug introduced with fq_req changes (ignored protocol and port)
				08 Sep 2014 - Fixed bugs with tcp oriented proto steering.
				09 Sep 2014 - corrected buglette that was preventing udp:0 or tcp:0 from working. (steering)
				23 Sep 2014 - Added suport for multiple tables in order to keep local traffic off of the rate limit bridge.
				24 Sep 2014 - Added vlan_id support. Added support ITONS dscp demands.
				14 Oct 2014 - Added check to prevent ip2mac table from being overlaid if new table is empty.
				11 Nov 2014 - Added ability to append a suffix string to hostnames returned by openstack.
				13 Nov 2014 - Corrected out of bounds range exception in add_phost_suffix (when given a string with a single blank)
				16 Jan 2015 - Changes to allow transport port to cary a mask in addition to the port value.
				19 Jan 2015 - Limit the queue list to run only on hosts listed.
				26 Jan 2015 - Corrected table number problem -- outbound data should resub through the base+1 table, but inbound
					packets should resub through the base table, not base+1.
				01 Feb 2015 - Corrected bug itroduced when host name removed from fmod parmss (agent w/ ssh-broker changes).
				19 Feb 2015 - Change in adjust_queues_agent to allow create queues to be driven from agent without -h on command line.
				21 Mar 2015 - Changes to support new bandwith endpoint flow-mod agent script.
*/

package managers

import (
	//"bufio"
	//"errors"
	"encoding/json"
	"fmt"
	//"io"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/clike"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/gizmos"
)

// --- Private --------------------------------------------------------------------------

/*
	Ostack returns a list of hostnames which might map to the wrong network (management
	rather than ops), so if a phost suffix is defined in the config file, this function
	will augment each host in the list and add the suffix. Returns the new list, or
	the same list if phost_suffix is nil.
*/
func add_phost_suffix( old_list *string, suffix *string ) ( *string ) {
	if suffix == nil  || old_list == nil || *old_list == "" {
		return old_list
	}

	nlist := ""
	sep := ""

	htoks := strings.Split( *old_list, " " )
	for i := range htoks {
		if htoks[i] != "" {
			if (htoks[i])[0:1] >= "0" && (htoks[i])[0:1] <= "9" {
				nlist += sep + htoks[i]										// assume ip address, put on as is
			} else {
				if strings.Index( htoks[i], "." ) >= 0 {					// fully qualified name
					dtoks := strings.SplitN( htoks[i], ".", 2 )				// add suffix after first node in the name
					nlist += sep + dtoks[0] + *suffix  + "." + dtoks[1]		
				} else {
					nlist += sep + htoks[i] + *suffix
				}
			}
	
			sep = " "
		}
	}

	return &nlist
}

/*
	We depend on an external script to actually set the queues so this is pretty simple.

	hlist is the space separated list of hosts that the script should adjust queues on
	muc is the max utilisation commitment for any link in the network.
*/

/*
	Writes the list of queue adjustment information (we assume from net-mgr) to a randomly named
	file in /tmp. Then we invoke the command passed in via cmd_base giving it the file name
	as the only parameter.  The command is expected to delete the file when finished with
	it.  See netmgr for a description of the items in the list.

	This is old school and probably will be deprecated soon.
*/
func adjust_queues( qlist []string, cmd_base *string, hlist *string ) {
	var (
		err error
		cmd_str	string			// final command string (with data file name)
	)

	if hlist == nil {
		hlist = &empty_str
	}

	fname := fmt.Sprintf( "/tmp/tegu_setq_%d_%x_%02d.data", os.Getpid(), time.Now().Unix(), rand.Intn( 10 ) )
	fq_sheep.Baa( 2, "adjusting queues: creating %s will contain %d items", fname, len( qlist ) );

	f, err := os.Create( fname )
	if err != nil {
		fq_sheep.Baa( 0, "ERR: unable to create data file: %s: %s  [TGUFQM000]", fname, err )
		return
	}
	
	for i := range qlist {
		fq_sheep.Baa( 2, "writing queue info: %s", qlist[i] )
		fmt.Fprintf( f, "%s\n", qlist[i] )
	}

	err = f.Close( )
	if err != nil {
		fq_sheep.Baa( 0, "ERR: unable to create data file (close): %s: %s  [TGUFQM001]", fname, err )
		return
	}

	fq_sheep.Baa( 1, "executing: sh %s -d %s %s", *cmd_base, fname, *hlist )
	cmd := exec.Command( shell_cmd, *cmd_base, "-d", fname, *hlist )
	err = cmd.Run()
	if err != nil  {
		fq_sheep.Baa( 0, "ERR: unable to execute set queue command: %s: %s  [TGUFQM002]", cmd_str, err )
	} else {
		fq_sheep.Baa( 1, "queues adjusted via %s", *cmd_base )
	}
}


/*
	Builds one setqueue json request per host and sends it to the agent. If there are
	multiple agents attached, the individual messages will be fanned out across the
	available agents, otherwise the agent will just process them sequentially which
	would be the case if we put all hosts into the same message.

	This now augments the switch name with the suffix; needs to be fixed for q-full
	so that it handles intermediate names properly.

	In the world with the ssh-broker, there is no -h host on the command line and
	the script's view of host name might not have the suffix that we are supplied
	with.  To prevent the script from not recognising an entry, we must now
	put an entry for both the host name and hostname+suffix into the list.
*/
func adjust_queues_agent( qlist []string, hlist *string, phsuffix *string ) {
	var (
		qjson	string						// final full json blob
		qjson_pfx	string					// static prefix
		sep = ""
	)

	target_hosts := make( map[string]bool )					// hosts that are actually affected by the queue list
	if phsuffix != nil {									// need to convert the host names in the list to have suffix
		nql := make( []string, len( qlist ) * 2 )			// need one for each possible host name

		offset := len( qlist )								// put the originals into the second half of the array
		for i := range qlist {
			nql[offset+i] = qlist[i]								// just copy the original

			toks := strings.SplitN( qlist[i], "/", 2 )				// split host from front
			if len( toks ) == 2 {
				nh := add_phost_suffix( &toks[0],  phsuffix )		// add the suffix
				nql[i] = *nh + "/" +  toks[1]
				target_hosts[*nh] = true
			} else {
				nql[i] = qlist[i]
				fq_sheep.Baa( 1, "target host not snarfed: %s", qlist[i] )
			}
		}

		qlist = nql
	} else {												// just snarf the list of hosts affected
		for i := range qlist {
			toks := strings.SplitN( qlist[i], "/", 2 )				// split host from front
			if len( toks ) == 2 {
				target_hosts[toks[0]] = true
			}
		}
	}

	fq_sheep.Baa( 1, "adjusting queues:  sending %d queue setting items to agents",  len( qlist ) );

	qjson_pfx = `{ "ctype": "action_list", "actions": [ { "atype": "setqueues", "qdata": [ `

	for i := range qlist {
		fq_sheep.Baa( 2, "queue info: %s", qlist[i] )
		qjson_pfx+= fmt.Sprintf( "%s%q", sep, qlist[i] )
		sep = ", "
	}

	qjson_pfx+= ` ], "hosts": [ `

	sep = ""
	for h := range target_hosts {			// build one request per host and send to agents -- multiple ageents then these will fan out
		qjson = qjson_pfx					// seed the next request with the constant prefix
		qjson += fmt.Sprintf( "%s%q", sep, h )

		qjson += ` ] } ] }`
	
		fq_sheep.Baa( 2, "queue update: host=%s %s", h, qjson )
		tmsg := ipc.Mk_chmsg( )
		tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, qjson, nil )		// send this as a short request to one agent
	}
}

/*
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
func send_bw_fmods( data *Fq_req, phost_suffix *string ) {
	if data.Espq.Switch == "" {									// we must have a switch name to set bandwidth fmods
		fq_sheep.Baa( 1, "unable to send bw-fmods request to agent: no switch defined in input data" )
		return
	}

	data.Match.Smac = data.Match.Ip1				// we send the source endpoint uuid to let agent convert and find vlan and ofport
	fq_sheep.Baa( 2, "src endpoing: %s", *data.Match.Smac )

	mac2 := epid2mac( data.Match.Ip2 )				// must convert the 'remote' endpoint to a real mac as agent on phost won't have uuid knowledge
	if mac2 == "" {
		fq_sheep.Baa( 1, "could not map endpoint id (2) to mac: %s", *data.Match.Ip2 )
		return
	}
	data.Match.Dmac = &mac2
	fq_sheep.Baa( 2, "dest mac address mapped: %s ==> %s", *data.Match.Ip2, mac2 )

	host := &data.Espq.Switch 									// Espq.Switch has real name (host) of switch
	if phost_suffix != nil {										// we need to add the physical host suffix
		host = add_phost_suffix( host, phost_suffix )
	}
	

	//FIXME:  do we? must add a way to pass IP addresses on match

	//data.Match.Smac = ip2mac[*data.Match.Ip1]					// res-mgr thinks in IP, flow-mods need mac; convert
	//data.Match.Dmac = ip2mac[*data.Match.Ip2]					// add to data for To_bw_map() call later

	msg := &agent_cmd{ Ctype: "action_list" }					// create a message for agent manager to send to an agent
	msg.Actions = make( []action, 1 )							// just a single action
	msg.Actions[0].Atype = "bw_fmod"							// set all related bandwidth flow-mods for an endpoint
	msg.Actions[0].Hosts = make( []string, 1 )					// bw endpoint flow-mods created on just one host
	msg.Actions[0].Hosts[0] = *host
	msg.Actions[0].Data = data.To_bw_map()						// convert useful data from caller into parms for agent

	json, err := json.Marshal( msg )						// bundle into a json string
	if err != nil {
		fq_sheep.Baa( 0, "unable to build json to set flow mod: %s", err )
	} else {
		tmsg := ipc.Mk_chmsg( )
		tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, string( json ), nil )		// send as a short request to one agent
	}

	fq_sheep.Baa( 2, "bandwidth endpoint flow-mod request sent to agent manager: %s", json )
}

/*
	Send a bandwidth endpoint flow-mod request for a oneway set of
	flow-mods.  This is little more than a wrapper that converts
	the fq_req into an agent request. The ultimate agent action is
	to put in all needed flow-mods on the endpoint host in one go,
	so no need for individual requests for each.

	Yes, this probably _could_ be pushed up into the reservation manager;
	see comments above.
*/
func send_bwow_fmods( data *Fq_req, phost_suffix *string ) {
	if data == nil {
		fq_sheep.Baa( 1, "fq_req: internal mishap: unable to send bwow-fmods data to bwow function was nil" )
		return
	}

	if data.Espq == nil || data.Espq.Switch == "" {									// we must have a switch name to set bandwidth fmods
		fq_sheep.Baa( 1, "unable to send bwow-fmods request to agent: no switch defined in input data" )
		return
	}

	host := &data.Espq.Switch 									// Espq.Switch has real name (host) of switch
	if phost_suffix != nil {									// we need to add the physical host suffix
		host = add_phost_suffix( host, phost_suffix )
	}

	/*
	mac1 := epid2mac( data.Match.Ip1 )
	if mac1 == "" {
		fq_sheep.Baa( 1, "oneway: unable to map endpoint (1) uuid (%s) to mac address", *data.Match.Ip1 )
		return
	}
	data.Match.Smac = &mac1
	*/
	data.Match.Smac = data.Match.Ip1							// we pass the endpoint uuid and let agent convert to mac/vlan/ofport tuple
	

	mac2 := ""
	if data.Match.Ip2 != nil {														// if ep2 is external, then it's ok to be nil
		//--deprecated data.Match.Dmac = ip2mac[*data.Match.Ip2]					// this may come up nil and that's ok
		mac2 = epid2mac( data.Match.Ip2 )											// dest must be converted to mac as agent won't have remote phost info
		if mac2 == "" {																// but if it comes in, it better xlate
			fq_sheep.Baa( 1, "oneway: unable to map endpoint (2) uuid (%s) to mac address", *data.Match.Ip1 )
			return
		}

		data.Match.Dmac = &mac2
	} else {
		data.Match.Dmac = nil
	}

	msg := &agent_cmd{ Ctype: "action_list" }					// create a message for agent manager to send to an agent
	msg.Actions = make( []action, 1 )							// just a single action
	msg.Actions[0].Atype = "bwow_fmod"							// operation to invoke on agent
	msg.Actions[0].Hosts = make( []string, 1 )					// oneway flow-mods created on just one host
	msg.Actions[0].Hosts[0] = *host
	msg.Actions[0].Data = data.To_bwow_map()					// convert useful data from caller into parms for agent

	json, err := json.Marshal( msg )						// bundle into a json string
	if err != nil {
		fq_sheep.Baa( 0, "unable to build json to set bwow flow mod" )
	} else {
		tmsg := ipc.Mk_chmsg( )
		tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, string( json ), nil )		// send as a short request to one agent
	}

	fq_sheep.Baa( 2, "oneway bandwidth flow-mod request sent to agent manager: %s", json )
}

/*
	WARNING: this should be deprecated.  Still needed by steering, but that should change. Tegu
		should send generic 'setup' actions to the agent and not try to craft flow-mods.
		Thus, do NOT use this from any new code!

	Send a flow-mod to the agent using a generic struct to represnt the match and action criteria.

	The fq_req contains data that are neither match or action specific (priority, expiry, etc) or
	are single purpose (match or action only) e.g. late binding mac value. It also contains  a set
	of match and action paramters that are applied depending on where they are found.
	Data expected in the fq_req:
		Nxt_mac - the mac address that is to be set on the action as dest (steering)
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

	phsuffix is the physical host suffix that must be added to each host endpoint name.

	TODO: this needs to be expanded to be generic and handle all possible match/action parms
		not just the ones that are specific to res and/or steering.  It will probably need
		an on-all flag in the main request struct rather than deducing it from parms.
*/
func send_gfmod_agent( data *Fq_req, ip2mac map[string]*string, hlist *string, phsuffix *string ) {

	if data == nil {
		return
	}

	if data.Pri <= 0 {
		data.Pri = 100
	}

	timeout := int64( 0 )									// never expiring if expiry isn't given
	if data.Expiry > 0 {
		timeout = data.Expiry - time.Now().Unix()			// figure the timeout and skip if invalid
	}
	if timeout < 0 {
		fq_sheep.Baa( 1, "timeout for flow-mod was too small, not generated: %d", timeout )
		return
	}

	table := ""
	if data.Table > 0 {
		table = fmt.Sprintf( "-T %d ", data.Table )
	}

	match_opts := "--match"					// build match options

	if data.Match.Meta != nil {
		if *data.Match.Meta != "" {
			match_opts += " -m " + *data.Match.Meta
		}
	}

	if data.Match.Swport > 0  {						// valid port
		match_opts += fmt.Sprintf( " -i %d", data.Match.Swport )
	} else {
		if data.Match.Swport == -128 {				// late binding port, we sub in the late binding MAC that was given
			if data.Lbmac != nil {
				match_opts += fmt.Sprintf( " -i %s", *data.Lbmac )
			} else {
				fq_sheep.Baa( 1, "ERR: creating fmod: late binding port supplied, but late binding MAC was nil  [TGUFQM004]" )
			}
		}
	}

	smac := data.Match.Smac								// smac wins if both smac and sip are given
	if smac == nil {
		if data.Match.Ip1 != nil {						// src supplied, match on src
			smac = ip2mac[*data.Match.Ip1]
			if smac == nil {
				fq_sheep.Baa( 0, "ERR: cannot set fmod: src IP did not translate to MAC: %s  [TGUFQM005]", *data.Match.Ip1 )
				fq_sheep.Baa( 1, "ip2mac has %d entries", len( ip2mac ) )
				return
			}
		}
	}
	if smac != nil {
		match_opts += " -s " + *smac
	}

	dmac := data.Match.Dmac								// dmac wins if both dmac and sip are given
	if dmac == nil {
		if data.Match.Ip2 != nil {						// src supplied, match on src
			dmac = ip2mac[*data.Match.Ip2]
			if dmac == nil {
				fq_sheep.Baa( 0, "ERR: cannot set fmod: dst IP did not translate to MAC: %s  [TGUFQM006]", *data.Match.Ip2 )
				return
			}
		}
	}
	if dmac != nil {
		match_opts += " -d " + *dmac
	}

	if *data.Match.Tpsport != "0" {
		match_opts += fmt.Sprintf( " -p %s:%s", *data.Tptype, *data.Match.Tpsport )
	}

	if *data.Match.Tpdport != "0" {
		match_opts += fmt.Sprintf( " -P %s:%s", *data.Tptype, *data.Match.Tpdport )
	}

	if data.Extip != nil  &&   *data.Extip != "" {					// an external IP address must be matched in addition to gw mac
		match_opts += " " + *data.Exttyp + " " + *data.Extip		// caller must set the direction (-S or -D) as we don't know
	}

	if data.Match.Dscp >= 0  {
		match_opts += fmt.Sprintf( " -T %d", data.Match.Dscp << 2 )	// agent expects value shifted off of the TOS bits.
	}


	action_opts := "--action"										// build the action options

	if data.Action.Dmac != nil {						
		action_opts += " -d " + *data.Action.Dmac
	}
	if data.Action.Smac != nil {
		action_opts += " -s " + *data.Action.Smac
	}

	if data.Action.Vlan_id != nil {									// can be either a mac address (resolved by agent) or a real vlan
		if strings.Index( *data.Action.Vlan_id, "." ) > 0 {			// has dot -- assume it's an IP address with  leading project/
			action_opts += " -v " + *ip2mac[*data.Action.Vlan_id]	// assume its a [project/]IP rather than a mac
		} else {
			action_opts += " -v " + *data.Action.Vlan_id			// else it's a mac or value and can be sent as is
		}
	}

	if data.Nxt_mac != nil {								// ??? is this really needed; steering should just set the dest in action
		action_opts += " -d " + *data.Nxt_mac				// change the dest for steering if next hop supplied
	}

	if data.Action.Dscp >= 0  && data.Match.Dscp != data.Action.Dscp {	// no need to set it if it's what we matched on{
		action_opts += fmt.Sprintf( " -T %d", data.Action.Dscp << 2 )	// MUST shift; agent expects dscp to have lower two bits as 0
	}

	if data.Espq != nil && data.Espq.Queuenum >= 0 {
		action_opts += fmt.Sprintf( " -q %d", data.Espq.Queuenum )
	}

	if data.Action.Meta != nil {
		if *data.Action.Meta != "" {
			action_opts += " -m " + *data.Action.Meta
		}
	}

	output := "-N"												// output default to none
	if data.Output != nil {
		switch *data.Output {
			case "none":		output = "-N"
			case "normal":		output = "-n"
			case "drop":		output = "-X"

			default:
				fq_sheep.Baa( 1, "WRN: defaulting to no output: unknown fmod-output type specified: %s  [TGUFQM007]", *data.Output )
		}
	}
	if data.Resub != nil {				 						// action options order may be sensitive; ensure -R is last
		toks := strings.Split( *data.Resub, " " )
		for i := range toks {
			action_opts += " -R ," + toks[i]
		}

		output = "-N"											// for resub there is no output or resub doesn't work (override Output if given)
	}


	action_opts = fmt.Sprintf( "%s %s", action_opts, output )		// set up actions

	// ---- end building the fmod parms, now build an agent message and send it to agent manager to send -------------
	//base_json := `{ "ctype": "action_list", "actions": [ { "atype": "flowmod", "fdata": [ `

	//FIX-ME:  This check _should_ be based on Espq.Switch and not swid but need to confirm that nothing is sending
	//			with nil swid and something like br-int in Espq.Switch first.
	// 			When this is changed, res_mgr will be affected in table_9x_fmods().
	if data.Swid == nil {											// blast the fmod to all known hosts if a single target is not named
		hosts := strings.Split( *hlist, " " )
		for i := range hosts {
			tmsg := ipc.Mk_chmsg( )									// must have one per since we dont wait for an ack

			msg := &agent_cmd{ Ctype: "action_list" }				// create an agent message
			msg.Actions = make( []action, 1 )
			msg.Actions[0].Atype = "flowmod"
			msg.Actions[0].Hosts = make( []string, 1 )
			msg.Actions[0].Hosts[0] = hosts[i]
			msg.Actions[0].Fdata = make( []string, 1 )
			msg.Actions[0].Fdata[0] = fmt.Sprintf( `%s -t %d -p %d %s %s add 0x%x %s`, table, timeout, data.Pri, match_opts, action_opts, data.Cookie, data.Espq.Switch )

			json, err := json.Marshal( msg )			// bundle into a json string
			if err != nil {
				fq_sheep.Baa( 0, "unable to build json to set flow mod" )
			} else {
				fq_sheep.Baa( 2, "json: %s", json )
				tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, string( json ), nil )		// send as a short request to one agent
			}
		}
	} else {															// fmod goes only to the named switch
		sw_name := &data.Espq.Switch 									// Espq.Switch has real name (host) of switch
		if phsuffix != nil {											// we need to add the physical host suffix
			sw_name = add_phost_suffix( sw_name, phsuffix ) 			// TODO: this needs to handle intermediate switches properly; ok for Q-lite, but not full
		}
	
		msg := &agent_cmd{ Ctype: "action_list" }				// create an agent message
		msg.Actions = make( []action, 1 )
		msg.Actions[0].Atype = "flowmod"
		msg.Actions[0].Hosts = make( []string, 1 )
		msg.Actions[0].Hosts[0] = *sw_name
		msg.Actions[0].Fdata = make( []string, 1 )
		msg.Actions[0].Fdata[0] = fmt.Sprintf( `%s -t %d -p %d %s %s add 0x%x %s`, table, timeout, data.Pri, match_opts, action_opts, data.Cookie, *data.Swid )	
		json, err := json.Marshal( msg )						// bundle into a json string
		if err != nil {
			fq_sheep.Baa( 0, "unable to build json to set flow mod" )
		} else {
			fq_sheep.Baa( 2, "json: %s", json )
			tmsg := ipc.Mk_chmsg( )
			tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, string( json ), nil )		// send as a short request to one agent
		}
	}
}


/*
	Send a newly arrived host list to the agent manager.
*/
func send_hlist_agent( hlist *string ) {
	tmsg := ipc.Mk_chmsg( )
	tmsg.Send_req( am_ch, nil, REQ_CHOSTLIST, hlist, nil )			// push the list; does not expect response back
}


/*
	Send a request to openstack interface for an ip to mac map. We will _not_ wait on it
	and will handle the response in the main loop.

	Deprecated with lazy update -- a push on our behalf is requested at reservation time
*/
func req_ip2mac(  rch chan *ipc.Chmsg ) {
	fq_sheep.Baa( 2, "requesting host list from osif" )

	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, rch, REQ_IP2MACMAP, nil, nil )
}


// --- Public ---------------------------------------------------------------------------


/*
	the main go routine to act on messages sent to our channel. We expect messages from the
	reservation manager, and from a tickler that causes us to evaluate the need to resize
	ovs queues.

	DSCP values:  Dscp values range from 0-64 decimal, but when described on or by
		flow-mods are shifted two bits to the left. The send flow mod function will
		do the needed shifting so all values outside of that one funciton should assume/use
		decimal values in the range of 0-64.

*/
func Fq_mgr( my_chan chan *ipc.Chmsg, sdn_host *string ) {

	var (
		uri_prefix	string = ""
		msg			*ipc.Chmsg
		data		[]interface{}			// generic list of data on some requests
		fdata		*Fq_req					// flow-mod request data
		qcheck_freq	int64 = 5
		hcheck_freq	int64 = 180
		host_list	*string					// current set of openstack real hosts
		ip2mac		map[string]*string		// translation from ip address to mac
		switch_hosts *string				// from config file and overrides openstack list if given (mostly testing)
		ssq_cmd		*string					// command string used to set switch queues (from config file)
		//send_all	bool = false			// send all flow-mods; false means send just ingress/egress and not intermediate switch f-mods
		//alt_table	int = DEF_ALT_TABLE		// meta data marking table
		phost_suffix *string = nil			// physical host suffix added to each host name in the list from openstack (config)

		//max_link_used	int64 = 0			// the current maximum link utilisation
	)

	fq_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	fq_sheep.Set_prefix( "fq_mgr" )
	tegu_sheep.Add_child( fq_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	// -------------- pick up config file data if there --------------------------------
	/* ---- deprecated
	if sdn_host != nil && *sdn_host == "" {													// not supplied on command line, pull from config	
		if sdn_host = cfg_data["default"]["sdn_host"];  sdn_host == nil {	// no default; when not in config, then it's turned off and we send to agent
			sdn_host = &empty_str
		}
	}
	---- */
	sdn_host = &empty_str		// FIXME

	if cfg_data["default"]["queue_type"] != nil {					
		/*
		if *cfg_data["default"]["queue_type"] == "endpoint" {
			send_all = false
		} else {
			send_all = true
		}
		*/
	}
	/*
	if p := cfg_data["default"]["alttable"]; p != nil {			// this is the base; we use alt_table to alt_table + (n-1) when we need more than 1 table
		alt_table = clike.Atoi( *p )
	}
	*/
	

	if cfg_data["fqmgr"] != nil {								// pick up things in our specific setion
		if dp := cfg_data["fqmgr"]["ssq_cmd"]; dp != nil {		// set switch queue command
			ssq_cmd = dp
		}
	
/*
		if p := cfg_data["fqmgr"]["default_dscp"]; p != nil {		// this is a single value and should not be confused with the dscp list in the default section of the config
			dscp = clike.Atoi( *p )
		}
*/

		if p := cfg_data["fqmgr"]["queue_check"]; p != nil {		// queue check frequency from the control file
			qcheck_freq = clike.Atoi64( *p )
			if qcheck_freq < 5 {
				qcheck_freq = 5
			}
		}
	
		if p := cfg_data["fqmgr"]["host_check"]; p != nil {		// frequency of checking for new _real_ hosts from openstack
			hcheck_freq = clike.Atoi64( *p )
			if hcheck_freq < 30 {
				hcheck_freq = 30
			}
		}
	
		if p := cfg_data["fqmgr"]["switch_hosts"]; p != nil {
			switch_hosts = p;
		}
	
		if p := cfg_data["fqmgr"]["verbose"]; p != nil {
			fq_sheep.Set_level(  uint( clike.Atoi( *p ) ) )
		}

		if p := cfg_data["fqmgr"]["phost_suffix"]; p != nil {		// suffix added to physical host strings for agent commands
			if *p != "" {
				phost_suffix = p
				fq_sheep.Baa( 1, "physical host names will be suffixed with: %s", *phost_suffix )
			}
		}
	}
	// ----- end config file munging ---------------------------------------------------

	//tklr.Add_spot( qcheck_freq, my_chan, REQ_SETQUEUES, nil, ipc.FOREVER );  	// tickle us every few seconds to adjust the ovs queues if needed

	if switch_hosts == nil {
		tklr.Add_spot( 2, my_chan, REQ_CHOSTLIST, nil, 1 )  						// tickle once, very soon after starting, to get a host list
		tklr.Add_spot( hcheck_freq, my_chan, REQ_CHOSTLIST, nil, ipc.FOREVER )  	// tickles us every once in a while to update host list
		fq_sheep.Baa( 2, "host list will be requested from openstack every %ds", hcheck_freq )
	} else {
		host_list = switch_hosts
		fq_sheep.Baa( 0, "static host list from config used for setting OVS queues: %s", *host_list )
	}

	if sdn_host != nil  &&  *sdn_host != "" {
		uri_prefix = fmt.Sprintf( "http://%s", *sdn_host )
	}

	fq_sheep.Baa( 1, "flowmod-queue manager is running, sdn host: %s", *sdn_host )
	for {
		msg = <- my_chan					// wait for next message
		msg.State = nil						// default to all OK
		
		fq_sheep.Baa( 3, "processing message: %d", msg.Msg_type )
		switch msg.Msg_type {
			case REQ_GEN_FMOD:							// generic fmod; just pass it along w/o any special handling
				if msg.Req_data != nil {
					fdata = msg.Req_data.( *Fq_req ); 		// pointer at struct with all of our expected goodies
					send_gfmod_agent( fdata,  ip2mac, host_list, phost_suffix )
				}

			case REQ_BWOW_RESERVE:						// oneway bandwidth flow-mod generation
				msg.Response_ch = nil					// nothing goes back from this
				fdata = msg.Req_data.( *Fq_req ); 		// pointer at struct with all of the expected goodies
				send_bwow_fmods( fdata, phost_suffix )

			case REQ_BW_RESERVE:						// bandwidth endpoint flow-mod creation; single agent script creates all needed fmods
				fdata = msg.Req_data.( *Fq_req ); 		// pointer at struct with all of the expected goodies
				send_bw_fmods( fdata, phost_suffix )
				//send_bw_fmods( fdata, ip2mac, phost_suffix )
				msg.Response_ch = nil					// nothing goes back from this

			case REQ_IE_RESERVE:						// proactive ingress/egress reservation flowmod  (this is likely deprecated as of 3/21/2015 -- resmgr invokes the bw_fmods script via agent)
				fq_sheep.Baa( 0, "CRI: invalid fqmgr function (reserve) invoked and was ignored" )
				msg.Response_data = nil
				if msg.Response_ch != nil {
					msg.State = fmt.Errorf( "deprecated request ignored (reserve)" )
				}

			case REQ_ST_RESERVE:							// reservation fmods for traffic steering
				msg.Response_ch = nil						// for now, nothing goes back
				if msg.Req_data != nil {
					fq_data := msg.Req_data.( *Fq_req ); 			// request data
					if uri_prefix != "" {							// an sdn controller -- skoogi -- is enabled (not supported)
						fq_sheep.Baa( 0, "ERR: steering reservations are not supported with skoogi (SDNC); no flow-mods pushed" )
					} else {
						send_stfmod_agent( fq_data, ip2mac, host_list )	
					}
				} else {
					fq_sheep.Baa( 0, "CRI: missing data on st-reserve request to fq-mgr" )
				}

			case REQ_SK_RESERVE:							// send a reservation to skoogi
				data = msg.Req_data.( []interface{} ); 		// msg data expected to be array of interface: h1, h2, expiry, queue h1/2 must be IP addresses
				if uri_prefix != "" {
					fq_sheep.Baa( 2,  "msg to reserve: %s %s %s %d %d",  uri_prefix, data[0].(string), data[1].(string), data[2].(int64), data[3].(int) )
					msg.State = gizmos.SK_reserve( &uri_prefix, data[0].(string), data[1].(string), data[2].(int64), data[3].(int) )
				} else {
					fq_sheep.Baa( 1, "reservation not sent, no sdn-host defined:  %s %s %s %d %d",  uri_prefix, data[0].(string), data[1].(string), data[2].(int64), data[3].(int) )
				}

			case REQ_SETQUEUES:								// request from reservation manager which indicates something changed and queues need to be reset
				qlist := msg.Req_data.( []interface{} )[0].( []string )
				if ssq_cmd != nil {
					adjust_queues( qlist, ssq_cmd, host_list ) 					// if writing to a file and driving a local script
				} else {
					adjust_queues_agent( qlist, host_list, phost_suffix )		// if sending json to an agent
				}

			case REQ_CHOSTLIST:								// this is tricky as it comes from tickler as a request, and from osifmgr as a response, be careful!
				msg.Response_ch = nil;						// regardless of source, we should not reply to this request

				if msg.State != nil || msg.Response_data != nil {				// response from ostack if with list or error
					if  msg.Response_data.( *string ) != nil {
						hls := strings.TrimLeft( *(msg.Response_data.( *string )), " \t" )		// ditch leading whitespace
						hl := &hls
						if *hl != ""  {
							host_list = hl										// ok to use it
							if phost_suffix != nil {
								fq_sheep.Baa( 2, "host list from osif before suffix added: %s", *host_list )
								host_list = add_phost_suffix( host_list, phost_suffix )		// in some cases ostack sends foo, but we really need to use foo-suffix (sigh)
							}
							send_hlist_agent( host_list )							// send to agent_manager
							fq_sheep.Baa( 2, "host list received from osif: %s", *host_list )
						} else {
							fq_sheep.Baa( 1, "host list received from osif was discarded: ()" )
						}
					} else {
						fq_sheep.Baa( 0, "WRN: no  data from openstack; expected host list string  [TGUFQM009]" )
					}
				} else {
					req_hosts( my_chan, fq_sheep )					// send requests to osif for data
				}

			case REQ_IP2MACMAP:								// a new map from osif
				if  msg.Req_data != nil {
					newmap := msg.Req_data.( map[string]*string )
					if len( newmap ) > 0  {
						ip2mac = newmap										// safe to replace
						fq_sheep.Baa( 2, "ip2mac translation received from osif: %d elements", len( ip2mac ) )
					}else {
						if ip2mac != nil {
							fq_sheep.Baa( 2, "ip2mac translation received from osif: 0 elements -- kept old table with %d elements", len( ip2mac ) )
						} else {
							fq_sheep.Baa( 2, "ip2mac translation received from osif: 0 elements -- no existing table to keep" )
						}
					}
				} else {
					fq_sheep.Baa( 0, "WRN: no  data from osif (nil map); expected ip2mac translation map  [TGUFQM010]" )
				}
				msg.State = nil								// state is always good

			default:
				fq_sheep.Baa( 1, "unknown request: %d", msg.Msg_type )
				msg.Response_data = nil
				if msg.Response_ch != nil {
					msg.State = fmt.Errorf( "unknown request (%d)", msg.Msg_type )
				}
		}

		fq_sheep.Baa( 3, "processing message complete: %d", msg.Msg_type )
		if msg.Response_ch != nil {			// if a reqponse channel was provided
			fq_sheep.Baa( 3, "sending response: %d", msg.Msg_type )
			msg.Response_ch <- msg			// send our result back to the requestor
		}
	}
}
