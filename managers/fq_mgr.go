// vi: sw=4 ts=4:

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

*/

package managers

import (
	//"bufio"
	//"errors"
	"fmt"
	//"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"forge.research.att.com/gopkgs/bleater"
	"forge.research.att.com/gopkgs/clike"
	"forge.research.att.com/gopkgs/ipc"
	"forge.research.att.com/tegu/gizmos"
)

// --- Private --------------------------------------------------------------------------


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
		fq_sheep.Baa( 0, "ERR: unable to create data file: %s: %s", fname, err )
		return
	}
	
	for i := range qlist {
		fq_sheep.Baa( 2, "writing queue info: %s", qlist[i] )
		fmt.Fprintf( f, "%s\n", qlist[i] )
	}

	err = f.Close( )
	if err != nil {
		fq_sheep.Baa( 0, "ERR: unable to create data file (close): %s: %s", fname, err )
		return
	}

	fq_sheep.Baa( 1, "executing: sh %s -d %s %s", *cmd_base, fname, *hlist )
	cmd := exec.Command( shell_cmd, *cmd_base, "-d", fname, *hlist )
	err = cmd.Run()
	if err != nil  {
		fq_sheep.Baa( 0, "ERR: unable to execute set queue command: %s: %s", cmd_str, err )
	} else {
		fq_sheep.Baa( 1, "queues adjusted via %s", *cmd_base )
	}
}


/*
	Builds one setqueue json request per host and sends it to the agent. If there are 
	multiple agents attached, the individual messages will be fanned out across the 
	available agents, otherwise the agent will just process them sequentially which
	would be the case if we put all hosts into the same message.
*/
func adjust_queues_agent( qlist []string, hlist *string ) {
	var (
		qjson	string			// final full json blob
		qjson_pfx	string		// static prefix
		sep = ""
	)

	fq_sheep.Baa( 1, "adjusting queues:  sending %d queue setting items to agents",  len( qlist ) );

	qjson_pfx = `{ "ctype": "action_list", "actions": [ { "atype": "setqueues", "qdata": [ `

	for i := range qlist {
		fq_sheep.Baa( 2, "queue info: %s", qlist[i] )
		qjson_pfx+= fmt.Sprintf( "%s%q", sep, qlist[i] )
		sep = ", "
	}

	qjson_pfx+= ` ], "hosts": [ `

	hosts := strings.Split( *hlist, " " )
	sep = ""
	for i := range hosts {			// build one request per host and send to agents -- multiple ageents then these will fan out
		qjson = qjson_pfx			// seed the next request with the constant prefix
		qjson += fmt.Sprintf( "%s%q", sep, hosts[i] )

		qjson += ` ] } ] }`
	
		tmsg := ipc.Mk_chmsg( )
		tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, qjson, nil )		// send this as a short request to one agent
	}
}

/*
	Send a flow-mod request to the agent. If send_all is false, then only ingress/egress
	flow-mods are set; those indicateing queue 1 are silently skipped. This is controlled
	by the queue_type setting in the config file. 

	act_type is either "add" or "del" as this can be used to delete flow-mods when a 
	reservation is canceled. 

	destip is the IP address of the destination if different than either ip1 or ip2. 
	This is used in the case of a split path where there are gateways between and 
	we need to match the mac of the gateway as the dest, but also match the destination
	ip to further limit the sessions which match. This string is assumed to have been
	prepended with the necessary type (-D or -S) as set by the sender of the request.

	mdscp is the dscp to match; a value of 0 causes no dscp value to be matched
	wdscp is the dscp value that should be written on the outgoing datagram.

	DSCP values are assumed to NOT have been shifted!
*/
func send_fmod_agent( act_type string, ip1 string, ip2 string, extip string, expiry int64, qnum int, sw string, port int, ip2mac map[string]*string, mdscp int, wdscp int, send_all bool ) {
	var (
		host	string
	)

	if send_all || qnum != 1 {					// ignore if skipping intermediate and this isn't ingress/egress
		m1 := ip2mac[ip1]						// set with mac addresses
		m2 := ip2mac[ip2]
		timeout := expiry - time.Now().Unix()	// figure the timeout and skip if too small
		if timeout > 15 {
			if port <= 0 {						// we'll assume that the switch is actually the phy host and br-int is what needs to be set
				host = sw
				sw = "br-int"
			} else {							// port known, so switch must ben known too
				host = "all"
			}
	
			qjson := `{ "ctype": "action_list", "actions": [ { "atype": "flowmod", "fdata": [ `

			fq_sheep.Baa( 1, "flow-mod: pri=400 tout=%d src=%s dest=%s extip=%s host=%s q=%d mT=%d aT=%d sw=%s", timeout, *m1, *m2, extip, host, qnum, mdscp, wdscp, sw )

			qjson += fmt.Sprintf( `"-h %s -t %d -p 400 --match -T %d -s %s -d %s `, host, timeout, mdscp, *m1, *m2 ) // MUST always match a dscp value to prevent loops on resubmit
			if extip != "" {
				qjson += fmt.Sprintf( `%s `,  extip )		// external IP is assumed to be prefixed with the correct -S or -D flag
			}

			qjson += fmt.Sprintf( `--action -q %d -T %d -R ,0 -N  %s 0xdead %s"`, qnum, wdscp << 2, act_type, sw )			// set queue and dscp value, then resub table 0 to match openstack fmod

			qjson += ` ] } ] }`

			tmsg := ipc.Mk_chmsg( )
fq_sheep.Baa( 2, ">>>> %s", qjson )
			tmsg.Send_req( am_ch, nil, REQ_SENDSHORT, qjson, nil )		// send as a short request to one agent
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
	Send a request to openstack interface for a host list. We will _not_ wait on it 
	and will handle the response in the main loop. 
*/
func req_hosts(  rch chan *ipc.Chmsg ) {
	fq_sheep.Baa( 2, "requesting host list from osif" )

	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, rch, REQ_CHOSTLIST, nil, nil )
}

/*
	Send a request to openstack interface for an ip to mac map. We will _not_ wait on it 
	and will handle the response in the main loop. 
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
		data		[]interface{}
		qcheck_freq	int64 = 5
		hcheck_freq	int64 = 180
		host_list	*string					// current set of openstack real hosts
		ip2mac		map[string]*string		// translation from ip address to mac
		switch_hosts *string				// from config file and overrides openstack list if given (mostly testing)
		ssq_cmd		*string					// command string used to set switch queues (from config file)
		send_all	bool = false			// send all flow-mods; false means send just ingress/egress and not intermediate switch f-mods
		dscp		int = 42	 			// generic diffserv value used to mark packets as priority

		//max_link_used	int64 = 0			// the current maximum link utilisation
	)

	fq_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	fq_sheep.Set_prefix( "fq_mgr" )
	tegu_sheep.Add_child( fq_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	// -------------- pick up config file data if there --------------------------------
	if *sdn_host == "" {													// not supplied on command line, pull from config	
		if sdn_host = cfg_data["default"]["sdn_host"];  sdn_host == nil {	// no default; when not in config, then it's turned off and we send to agent
			sdn_host = &empty_str
		}
	}

	if cfg_data["default"]["queue_type"] != nil {					
		if *cfg_data["default"]["queue_type"] == "endpoint" {
			send_all = false
		} else {
			send_all = true
		}
	}

	if cfg_data["fqmgr"] != nil {								// pick up things in our specific setion
		if dp := cfg_data["fqmgr"]["ssq_cmd"]; dp != nil {		// set switch queue command
			ssq_cmd = dp
		}
	
		if p := cfg_data["fqmgr"]["default_dscp"]; p != nil {		// this is a single value and should not be confused with the dscp list in the default section of the config
			dscp = clike.Atoi( *p )
		}

		if p := cfg_data["fqmgr"]["queue_check"]; p != nil {		// queue check frequency from the control file
			qcheck_freq = clike.Atoi64( *p )
			if qcheck_freq < 5 {
				qcheck_freq = 5
			}
		}
	
		if p := cfg_data["fqmgr"]["host_check"]; p != nil {		// frequency of checking for new _real_ hosts from openstack
			hcheck_freq = clike.Atoi64( *p )
			if hcheck_freq < 180 {
				hcheck_freq = 180
			}
		}
	
		if p := cfg_data["fqmgr"]["switch_hosts"]; p != nil {
			switch_hosts = p;
		} 
	
		if p := cfg_data["fqmgr"]["verbose"]; p != nil {
			fq_sheep.Set_level(  uint( clike.Atoi( *p ) ) )
		}
	}
	// ----- end config file munging ---------------------------------------------------

	//tklr.Add_spot( qcheck_freq, my_chan, REQ_SETQUEUES, nil, ipc.FOREVER );  	// tickle us every few seconds to adjust the ovs queues if needed

	if switch_hosts == nil {
		tklr.Add_spot( 2, my_chan, REQ_CHOSTLIST, nil, 1 );  						// tickle once, very soon after starting, to get a host list
		tklr.Add_spot( hcheck_freq, my_chan, REQ_CHOSTLIST, nil, ipc.FOREVER );  	// tickles us every once in a while to update host list
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
		msg.State = nil					// default to all OK
		
		fq_sheep.Baa( 3, "processing message: %d", msg.Msg_type )
		switch msg.Msg_type {
			case REQ_IE_RESERVE:							// the new proactive ingress/egress reservation format
				data = msg.Req_data.( []interface{} ); 		// msg data expected to be array of interface: h1, h2, expiry, *Spq
				spq := data[FQ_SPQ].( *gizmos.Spq )

				if uri_prefix != "" {						// an sdn controller -- skoogi -- is enabled
					msg.State = gizmos.SK_ie_flowmod( &uri_prefix, data[FQ_IP1].(string), data[FQ_IP2].(string), data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port )

					if msg.State == nil {				// no error, we are silent
						fq_sheep.Baa( 2,  "proactive reserve successfully sent: uri=%s h1=%s h2=%s exp=%d qnum=%d swid=%s port=%d dscp=%d",  
									uri_prefix, data[FQ_IP1].(string), data[FQ_IP2].(string), data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port, data[FQ_DSCP].(int) )
						msg.Response_ch = nil
					} else {
						// do we need to suss out the id and mark it failed, or set a timer on it,  so as not to flood reqmgr with errors?
						fq_sheep.Baa( 1,  "ERR: proactive reserve failed: uri=%s h1=%s h2=%s exp=%d qnum=%d swid=%s port=%d",  
									uri_prefix, data[FQ_IP1].(string), data[FQ_IP2].(string), data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port )
					}
				} else {
					//fq_sheep.Baa( 2,  "proactive reservation not sent, no sdn-host defined: uri=%s h1=%s h2=%s exp=%d qnum=%d swid=%s port=%d",  
						//uri_prefix, data[FQ_IP1].(string), data[FQ_IP2].(string), data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port )
					// three flowmods 
					extip := data[FQ_EXTIP].(string)
					if extip != "" {
						extip = data[FQ_EXTTY].(string) + " " + extip			// add the type qualifier if it's not empty
					}

					if data[FQ_DIR_IN].(bool)  {			// inbound to this switch we need to revert from our settings to the 'origianal' settings
						udscp := math.Abs( float64( data[FQ_DSCP].(int) ) )
						send_fmod_agent( "add", data[FQ_IP1].(string), data[FQ_IP2].(string), extip, data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port, ip2mac, dscp, int( udscp ), send_all )
						send_fmod_agent( "add", data[FQ_IP1].(string), data[FQ_IP2].(string), extip, data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port, ip2mac, 40, 32, send_all )
						send_fmod_agent( "add", data[FQ_IP1].(string), data[FQ_IP2].(string), extip, data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port, ip2mac, 41, 46, send_all )
					} else {								// outbound from this switch we need to translate dscp values to our values
						udscp := 0							// if DSCP is > 0 it's an 'exit' value that we leave set and translate 0 to dscp; if < 0 we assume we must match the abs() value
						if data[FQ_DSCP].(int) < 0 {
							udscp = -data[FQ_DSCP].(int)
						}
						send_fmod_agent( "add", data[FQ_IP1].(string), data[FQ_IP2].(string), extip, data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port, ip2mac, udscp, dscp, send_all )
						send_fmod_agent( "add", data[FQ_IP1].(string), data[FQ_IP2].(string), extip, data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port, ip2mac, 32, 40, send_all )
						send_fmod_agent( "add", data[FQ_IP1].(string), data[FQ_IP2].(string), extip, data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port, ip2mac, 46, 41, send_all )
					}
					msg.Response_ch = nil
				}

			case REQ_RESERVE:								// send a reservation to skoogi
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
					adjust_queues( qlist, ssq_cmd, host_list ) 	// if writing to a file and driving a local script
				} else {
					adjust_queues_agent( qlist, host_list )		// if sending json to an agent
				}

			case REQ_CHOSTLIST:								// this is tricky as it comes from tickler as a request, and from openstack as a response, be careful!
				msg.Response_ch = nil;						// regardless of source, we should not reply to this request

				if msg.State != nil || msg.Response_data != nil {				// response from ostack if with list or error
					if  msg.Response_data.( *string ) != nil {
						host_list = msg.Response_data.( *string )
						send_hlist_agent( host_list )							// send to agent_manager
						fq_sheep.Baa( 1, "host list received from osif: %s", *host_list )
					} else {
						fq_sheep.Baa( 0, "WRN: no  data from openstack; expected host list string" )
					}
				} else {
					fq_sheep.Baa( 2, "requesting lists from osif" )
					req_hosts( my_chan )					// send requests to osif for data
					req_ip2mac( my_chan )
				}

			case REQ_IP2MACMAP:								// caution: this  comes as a response; the request is generated by chostlist processing so we need 1 tickle
				msg.Response_ch = nil;						// regardless of source, we should not reply to this request

				if msg.State != nil || msg.Response_data != nil {				// response from ostack if with list or error
					if  msg.Response_data != nil {
						ip2mac = msg.Response_data.( map[string]*string )
						fq_sheep.Baa( 1, "ip2mac translation received from osif: %d elements", len( ip2mac ) )
					} else {
						fq_sheep.Baa( 0, "WRN: no  data from openstack; expected ip2mac translation map" )
					}
				}

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
