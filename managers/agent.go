// vi: sw=4 ts=4:

/*

	Mnemonic:	agent
	Abstract:	Manages everything associated with agents. Listens on the well known channel
				for requests from other tegu threads, and manages a separate data channel
				for agent input (none expected at this time.

	Date:		30 April 2014
	Author:		E. Scott Daniels

	Mods:		05 May 2014 : Added ability to receive and process json data from the agent
					and the function needed to process the output from a map_mac2phost request.
					Added ability to send the map_mac2phost request to the agent. 
				13 May 2014 : Added support for exit-dscp value.
				05 Jun 2014 : Fixed stray reference to net_sheep. 
				29 Oct 2014 : Corrected potential core dump if agent msg received is less than
					100 bytes.
				06 Jan 2015 : Added support for wide area (wacc)
*/

package managers

import (
	//"bufio"
	"encoding/json"
	//"flag"
	"fmt"
	//"io/ioutil"
	//"html"
	//"net/http"
	"os"
	"strings"
	//"time"

	"codecloud.web.att.com/gopkgs/bleater"
	"codecloud.web.att.com/gopkgs/clike"
	"codecloud.web.att.com/gopkgs/connman"
	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/gopkgs/jsontools"
	//"codecloud.web.att.com/tegu"
	//"codecloud.web.att.com/tegu/gizmos"
)

// ----- structs used to bundle into json commands

type action struct {			// specific action
	Atype	string				// something like map_mac2phost, or intermed_queues
	Aid		uint32				// request id used to map the response back to caller's request
	Hosts	[]string			// list of hosts to apply the action to
	Dscps	string				// space separated list of dscp values
	Data	map[string]string	// specific request data (parms likely)
}

type agent_cmd struct {			// overall command
	Ctype	string
	Actions []action
}

/*
	Manage things associated with a specific agent
*/
type agent struct {
	id		string
	jcache	*jsontools.Jsoncache				// buffered input resulting in 'records' that are complete json blobs
}

type agent_data struct {
	agents	map[string]*agent					// hash for direct index (based on ID string given to the session) 
	agent_list []*agent							// sequential index into map that allows easier round robin access for sendone
	aidx	int									// next spot in index for round robin sends 
}

/*
	Generic struct to unpack json received from an agent
*/
type agent_msg struct {
	Ctype	string			// command type -- should be response, ack, nack etc.
	Rtype	string			// type of response (e.g. map_mac2phost, or specific id for ack/nack)
	Rdata	[]string		// response data
	State	int				// if an ack/nack some state information 
	Vinfo	string			// agent verion (dbugging mostly)
	Rid		uint32			// the original request id
}

type pend_req struct {		// pending request -- something is expecting a response on a channel
	req		*ipc.Chmsg		// the original message (request) that will be sent back
	id		uint32			// our internal id for the request (put into the agent request and used as hash key)
}

// ---------------------------------------------------------------------------------------------------

/*
	Build the agent list from the map. The agent list is a 'sequential' list of all currently 
	connected agents which affords us an easy means to roundrobin through them. 
*/
func (ad *agent_data) build_list( ) {
	ad.agent_list = make( []*agent, len( ad.agents ) )
	i := 0
	for _, a := range ad.agents {
		ad.agent_list[i] = a
		i++
	}

	if ad.aidx >= i {			// wrap if list shrank and we point beyond it
		ad.aidx = 0
	}
}

/*
	Build an agent and add to our list of agents.
*/
func (ad *agent_data) Mk_agent( aid string ) ( na *agent ) {

	na = &agent{}
	na.id = aid
	na.jcache = jsontools.Mk_jsoncache()

	ad.agents[na.id] = na
	ad.build_list( )

	return
}

/*
	Send the message to one agent. The agent is selected using the current 
	index in the agent_data so that it effectively does a round robin.
*/
func (ad *agent_data) send2one( smgr *connman.Cmgr,  msg string ) {
	l := len( ad.agents ) 
	if l <= 0 {
		return
	}

	smgr.Write( ad.agent_list[ad.aidx].id, []byte( msg ) )
	ad.aidx++
	if ad.aidx >= l {
		if l > 1 {
			ad.aidx = 1		// skip the long running agent if more than one agent connected
		} else {
			ad.aidx = 0
		}
	}
}

/*
	Send the message to one agent. The agent is selected using the current 
	index in the agent_data so that it effectively does a round robin.
*/
func (ad *agent_data) sendbytes2one( smgr *connman.Cmgr,  msg []byte ) {
	l := len( ad.agents ) 
	if l <= 0 {
		return
	}
	
	smgr.Write( ad.agent_list[ad.aidx].id,  msg )
	ad.aidx++
	if ad.aidx >= l {
		if l > 1 {
			ad.aidx = 1		// skip the long running agent if more than one agent connected
		} else {
			ad.aidx = 0
		}
	}
}
/*
	Send the message to the designated 'long running' agent (lra); the
	agent that has been designated to handle all long running tasks
	that are not time sensitive (such as intermediate queue setup/checking).
	
*/
func (ad *agent_data) sendbytes2lra( smgr *connman.Cmgr,  msg []byte ) {
	l := len( ad.agents ) 
	if l <= 0 {
		return
	}
	
	smgr.Write( ad.agent_list[0].id,  msg )
}

/*
	Send the message to the designated 'long running' agent (lra); the
	agent that has been designated to handle all long running tasks
	that are not time sensitive (such as intermediate queue setup/checking).
	
*/
func (ad *agent_data) send2lra( smgr *connman.Cmgr,  msg string ) {
	l := len( ad.agents ) 
	if l <= 0 {
		return
	}
	
	smgr.Write( ad.agent_list[0].id,  []byte( msg ) )
}

/*
	Send the message to all agents.
*/
func (ad *agent_data) send2all( smgr *connman.Cmgr,  msg string ) {
	am_sheep.Baa( 2, "sending %d bytes", len( msg ) )
	for id := range ad.agents {
		smgr.Write( id, []byte( msg ) )
	}
}

/*
	Deal with incoming data from an agent. We add the buffer to the cahce
	(all input is expected to be json) and attempt to pull a blob of json
	from the cache. If the blob is pulled, then we act on it, else we 
	assume another buffer or more will be coming to complete the blob
	and we'll do it next time round.

	We should be synchronous through this function since it is called 
	directly by our main goroutine, thus it is safe to update the request
	tracker map directly (no locking).
*/
func ( a *agent ) process_input( buf []byte, rt_map map[uint32]*pend_req ) {
	var (
		req	agent_msg		// unpacked message struct
	)

	a.jcache.Add_bytes( buf )
	jblob := a.jcache.Get_blob()						// get next blob if ready
	for ; jblob != nil ; {
    	err := json.Unmarshal( jblob, &req )           // unpack the json 

		if err != nil {
			am_sheep.Baa( 0, "ERR: unable to unpack agent_message: %s  [TGUAGT000]", err )
			am_sheep.Baa( 2, "offending json: %s", string( buf ) )
		} else {
			am_sheep.Baa( 1, "%s/%s received from agent", req.Ctype, req.Rtype )
	
			switch( req.Ctype ) {					// "command type"
				case "response":					// response to a request
					switch( req.Rtype ) {
						case "map_mac2phost":												// map goes to network manager (no pending request)
							if req.State == 0 {
								msg := ipc.Mk_chmsg( )
								msg.Send_req( nw_ch, nil, REQ_MAC2PHOST, req.Rdata, nil )		// we don't expect a response
							} else {
								am_sheep.Baa( 1, "WRN: response for failed command received and ignored: %s  [TGUAGT002]", req.Rtype )
							}

						default:
							if req.Rid > 0 {
								pr := rt_map[req.Rid]
								if pr != nil {
									am_sheep.Baa( 2, "found request id in block and it mapped to a pending request: %d", req.Rid )
									msg := pr.req					// message block that was sent to us; fill out the response and return
									msg.Response_data = req.Rdata
									if req.State == 0 {
										msg.State = nil
									} else {
										msg.State = fmt.Errorf( "rc=%d", req.State )
									}

									delete( rt_map, req.Rid )						// done with the pending request block
									msg.Response_ch <- msg							// send response back to the process that caused the command to run
								} else {
									am_sheep.Baa( 1, "WRN: agent response ignored: request id in response didn't map to a pending request: %d [TGUAGTXXX]", req.Rid )   //FIX message id
								}
							} else {
								am_sheep.Baa( 1, "WRN: agent response didn't have a request id  or match a generic type: %s [TGUAGTXXX]", req.Rtype )   //FIX message id
							}
					}


				default:
					am_sheep.Baa( 1, "WRN:  unrecognised command type type from agent: %s  [TGUAGT003]", req.Ctype )
			}
		}

		jblob = a.jcache.Get_blob()								// get next blob if the buffer completed one and containe a second
	}

	return
}

//-------- request builders ---- (see agent_wa.go too) -----------------------------------------------------------------------------

/*
	Build a request to have the agent generate a mac to phost list and send it to one agent.
*/
func (ad *agent_data) send_mac2phost( smgr *connman.Cmgr, hlist *string ) {
	if hlist == nil || *hlist == "" {
		am_sheep.Baa( 2, "no host list, cannot request mac2phost" )
		return
	}
	
/*
	req_str := `{ "ctype": "action_list", "actions": [ { "atype": "map_mac2phost", "hosts": [ `
	toks := strings.Split( *hlist, " " )
	sep := " "
	for i := range toks {
		req_str += sep + `"` + toks[i] +`"`
		sep = ", "
	}

	req_str += ` ] } ] }`
*/

	msg := &agent_cmd{ Ctype: "action_list" }				// create command struct then convert to json
	msg.Actions = make( []action, 1 )
	msg.Actions[0].Atype = "map_mac2phost"
	msg.Actions[0].Aid = 0
	msg.Actions[0].Hosts = strings.Split( *hlist, " " )
	jmsg, err := json.Marshal( msg )			// bundle into a json string

	if err == nil {
		am_sheep.Baa( 3, "sending mac2phost request: %s", jmsg )
		ad.sendbytes2lra( smgr, jmsg )						// send as a long running request
	} else {
		am_sheep.Baa( 1, "WRN: unable to bundle mac2phost request into json: %s  [TGUAGT004]", err )
		am_sheep.Baa( 2, "offending json: %s", jmsg )
	}
}

/*
	Build a request to cause the agent to drive the setting of queues and fmods on intermediate bridges.
*/
func (ad *agent_data) send_intermedq( smgr *connman.Cmgr, hlist *string, dscp *string ) {
	if hlist == nil || *hlist == "" {
		return
	}
	
	msg := &agent_cmd{ Ctype: "action_list" }				// create command struct then convert to json
	msg.Actions = make( []action, 1 )
	msg.Actions[0].Atype = "intermed_queues"
	msg.Actions[0].Aid = 0
	msg.Actions[0].Hosts = strings.Split( *hlist, " " )
	msg.Actions[0].Dscps = *dscp

	jmsg, err := json.Marshal( msg )			// bundle into a json string

	if err == nil {
		am_sheep.Baa( 1, "sending intermediate queue setup request: hosts=%s dscp=%s", *hlist, *dscp )
		ad.sendbytes2lra( smgr, jmsg )						// send as a long running request
	} else {
		am_sheep.Baa( 0, "WRN: creating json intermedq command failed: %s  [TGUAGT005]", err )
	}
}

// ---------------- utility ------------------------------------------------------------------------

/*
	Accepts a string of space separated dscp values and returns a string with the values
	approprately shifted so that they can be used by the agent in a flow-mod command.  E.g.
	a dscp value of 40 is shifted to 160. 
*/
func shift_values( list string ) ( new_list string ) {
	new_list = ""
	sep := ""
	toks := strings.Split( list, " " )
	
	for i := range toks {
		n := clike.Atoi( toks[i] )
		new_list += fmt.Sprintf( "%s%d", sep, n<<2 )
		sep = " "
	}

	return
}

// ---------------- main agent goroutine -----------------------------------------------------------

func Agent_mgr( ach chan *ipc.Chmsg ) {
	var (
		port		string = "29055"					// port we'll listen on for connections
		adata		*agent_data
		host_list	string = ""
		dscp_list 	string = "46 26 18"					// list of dscp values that are used to promote a packet to the pri queue in intermed switches
		refresh 	int64 = 60
		iqrefresh 	int64 = 1800						// intermediate queue refresh
		req_id		uint32 = 1							// sync request id, key for hash (start at 1; 0 should never have an entry)
		req_track	map[uint32]*pend_req				// hash of pending requests
		type2name 	map[int]string						// map REQ_ types to a string that is passed as the command constant
		def_wan_uuid *string = nil						// default uuid for the wan (from config)
	)

	adata = &agent_data{}
	adata.agents = make( map[string]*agent )
	req_track = make( map[uint32]*pend_req )

	am_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	am_sheep.Set_prefix( "agentmgr" )
	tegu_sheep.Add_child( am_sheep )					// we become a child so that if the master vol is adjusted we'll react too

														// suss out config settings from our section
	if cfg_data["agent"] != nil {
		if p := cfg_data["agent"]["port"]; p != nil {
			port = *p
		}
		if p := cfg_data["agent"]["verbose"]; p != nil {
			am_sheep.Set_level( uint( clike.Atoi( *p ) ) )
		}
		if p := cfg_data["agent"]["refresh"]; p != nil {
			refresh = int64( clike.Atoi( *p ) )
		}
		if p := cfg_data["agent"]["iqrefresh"]; p != nil {
			iqrefresh = int64( clike.Atoi( *p ) )
			if iqrefresh < 90 {
				am_sheep.Baa( 1, "iqrefresh in configuration file is too small, set to 90 seconds" )
				iqrefresh = 90
			}
		}
		if p := cfg_data["agent"]["wan_uuid"]; p != nil {		// uuid that gets passed to the agent for the add-port call
			def_wan_uuid = p
			am_sheep.Baa( 1, "wan uuid will default to: %s", *def_wan_uuid )
		} else {
			dup_str := ""
			def_wan_uuid = &dup_str
		}
	}
	if cfg_data["default"] != nil {						// we pick some things from the default section too
		if p := cfg_data["default"]["pri_dscp"]; p != nil {			// list of dscp (diffserv) values that match for priority promotion
			dscp_list = *p
			am_sheep.Baa( 1, "dscp priority list from config file: %s", dscp_list )
		} else {
			am_sheep.Baa( 1, "dscp priority list not in config file, using defauts: %s", dscp_list )
		}
	}
	
	dscp_list = shift_values( dscp_list )				// must shift values before giving to agent
	
	type2name = make( map[int]string, 5 )
	type2name[REQ_WA_PORT] = "wa_port"					// command constants that get sent off to the agent
	type2name[REQ_WA_TUNNEL] = "wa_tunnel"
	type2name[REQ_WA_ROUTE]	= "wa_route"
	type2name[REQ_WA_DELCONN] = "wa_del_conn"

	am_sheep.Baa( 1,  "agent_mgr thread started: listening on port %s", port )

	tklr.Add_spot( 2, ach, REQ_MAC2PHOST, nil, 1 );  					// tickle once, very soon after starting, to get a mac translation
	tklr.Add_spot( 10, ach, REQ_INTERMEDQ, nil, 1 );		  			// tickle once, very soon, to start an intermediate refresh asap
	tklr.Add_spot( refresh, ach, REQ_MAC2PHOST, nil, ipc.FOREVER );  	// reocurring tickle to get host mapping 
	tklr.Add_spot( iqrefresh, ach, REQ_INTERMEDQ, nil, ipc.FOREVER );  	// reocurring tickle to ensure intermediate switches are properly set

	sess_chan := make( chan *connman.Sess_data, 1024 )					// channel for comm from agents (buffers, disconns, etc)
	smgr := connman.NewManager( port, sess_chan );
	

	for {
		select {							// wait on input from either channel
			case req := <- ach:
				req.State = nil				// nil state is OK, no error

				am_sheep.Baa( 3, "processing request %d", req.Msg_type )

				switch req.Msg_type {
					case REQ_NOOP:						// just ignore -- acts like a ping if there is a return channel

					case REQ_SENDALL:					// send request to all agents
						if req.Req_data != nil {
							adata.send2all( smgr,  req.Req_data.( string ) )
						}

					case REQ_SENDLONG:					// send a long request to one agent
						if req.Req_data != nil {
							adata.send2one( smgr,  req.Req_data.( string ) )
						}

					case REQ_SENDSHORT:					// send a short request to one agent (round robin)
						if req.Req_data != nil {
							adata.send2one( smgr,  req.Req_data.( string ) )
						}

					case REQ_MAC2PHOST:					// send a request for agent to generate  mac to phost map
						if host_list != "" {
							adata.send_mac2phost( smgr, &host_list )
						}

					case REQ_CHOSTLIST:					// a host list from fq-manager
						if req.Req_data != nil {
							host_list = *(req.Req_data.( *string ))
						}

					case REQ_INTERMEDQ:
						req.Response_ch = nil
						if host_list != "" {
							adata.send_intermedq( smgr, &host_list, &dscp_list )
						}
	
					case REQ_WA_PORT, REQ_WA_TUNNEL, REQ_WA_ROUTE, REQ_WA_DELCONN:	// wa commands can be setup/sent by a common function
						if req.Req_data != nil {
							req_track[req_id] = &pend_req {			// tracked request to have block when response recevied from agent
								req: req,
								id:	req_id,
							}

							adata.send_wa_cmd( type2name[req.Msg_type], smgr, req_track[req_id], def_wan_uuid )		// do the real work to push to agent
							req = nil									// prevent immediate response
							req_id++
							if req_id == 0 {
								req_id = 1
							}	
						} else {
							req.State = fmt.Errorf( "missing data on request to agent manager" )		// immediate failure
						}
				}

	
				if req != nil  &&  req.Response_ch != nil {				// if response needed; send the request (updated) back 
					am_sheep.Baa( 3, "processing request finished %d", req.Msg_type )			// we seem to wedge in network, this will be chatty, but may help
					req.Response_ch <- req
				}


			case sreq := <- sess_chan:		// data from a connection or TCP listener
				switch( sreq.State ) {
					case connman.ST_ACCEPTED:		// newly accepted connection; no action 

					case connman.ST_NEW:			// new connection
						a := adata.Mk_agent( sreq.Id )
						am_sheep.Baa( 1, "new agent: %s [%s]", a.id, sreq.Data )
						if host_list != "" {											// immediate request for this 
							adata.send_mac2phost( smgr, &host_list )
							adata.send_intermedq( smgr, &host_list, &dscp_list )
						}
				
					case connman.ST_DISC:
						am_sheep.Baa( 1, "agent dropped: %s", sreq.Id )
						if _, not_nil := adata.agents[sreq.Id]; not_nil {
							delete( adata.agents, sreq.Id )
						} else {
							am_sheep.Baa( 1, "did not find an agent with the id: %s", sreq.Id )
						}
						adata.build_list()			// rebuild the list to drop the agent
						
					case connman.ST_DATA:
						if _, not_nil := adata.agents[sreq.Id]; not_nil {
							cval := 100
							if len( sreq.Buf ) < 100 {						// don't try to go beyond if chop value too large
								cval = len( sreq.Buf )
							}
							am_sheep.Baa( 2, "data: [%s]  %d bytes received:  first 100b: %s", sreq.Id, len( sreq.Buf ), sreq.Buf[0:cval] )
							adata.agents[sreq.Id].process_input( sreq.Buf, req_track )
						} else {
							am_sheep.Baa( 1, "data from unknown agent: [%s]  %d bytes ignored:  %s", sreq.Id, len( sreq.Buf ), sreq.Buf )
						}
				}
		}			// end select
	}
}

