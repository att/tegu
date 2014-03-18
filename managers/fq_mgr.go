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

*/

package managers

import (
	//"bufio"
	//"errors"
	"fmt"
	//"io"
	"math/rand"
	"os"
	"os/exec"
	//"strings"
	"time"

	"forge.research.att.com/gopkgs/bleater"
	"forge.research.att.com/gopkgs/clike"
	"forge.research.att.com/gopkgs/ipc"
	"forge.research.att.com/tegu/gizmos"
)

//var (
// NO GLOBALS HERE; use globals.go
//)

// --------------------------------------------------------------------------------------

// --- Private --------------------------------------------------------------------------

/*
	This sends a request to network manager for the max link allocation value. 
	This is an async request and the response should come back to the main 
	channel so that we don't block. 
*/
/*
DEPRECATED with specific queues
func req_link_max( rch chan *ipc.Chmsg ) {
	
	req := ipc.Mk_chmsg( )
	req.Send_req( nw_ch, rch, REQ_GETLMAX, time.Now().Unix(), nil )
}
*/

/*
	In the intial 'blanket setting' mode we will set the priority queue on all switches
	in our domain to reflect the link that has the maximum utilisation commitment. 

	We depend on an external script to actually set the queues so this is pretty simple.

	hlist is the space separated list of hosts that the script should adjust queues on
	muc is the max utilisation commitment for any link in the network. 
*/
/*
DEPRECATED -- original
*/
func orig_adjust_queues( cmd_str *string, hlist *string, muc int64 ) ( err error ) {
	
	if hlist == nil {
		err = fmt.Errorf( "cannot adjust queues, no hosts in list" )
		return
	}

	fq_sheep.Baa( 1, "adjusting queues on: limit=%dM  %s", muc/1000000, *hlist )
	cmd := exec.Command( *cmd_str, fmt.Sprintf( "%d", muc ), *hlist )
	err = cmd.Run()

	return
}

/*
	Writes the list of queue adjustment information (we assume from net-mgr) to a randomly named
	file in /tmp. Then we invoke the command passed in via cmd_base giving it the file name
	as the only parameter.  The command is expected to delete the file when finished with 
	it.  See netmgr for a description of the items in the list. 
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
	send a request to openstack interface for a host list. We will _not_ wait on it 
	and will handle the response in the main loop. 
*/
func req_hosts(  rch chan *ipc.Chmsg ) {
	fq_sheep.Baa( 1, "requesting host list from osif" )

	req := ipc.Mk_chmsg( )
	req.Send_req( osif_ch, rch, REQ_CHOSTLIST, nil, nil )
}


// --- Public ---------------------------------------------------------------------------


/*
	the main go routine to act on messages sent to our channel. We expect messages from the 
	reservation manager, and from a tickler that causes us to evaluate the need to resize 
	ovs queues.

*/
func Fq_mgr( my_chan chan *ipc.Chmsg, sdn_host *string ) {

	var (
		uri_prefix	string = ""
		msg			*ipc.Chmsg
		data		[]interface{}
		qcheck_freq	int64 = 5
		hcheck_freq	int64 = 180
		host_list	*string					// current set of openstack real hosts
		switch_hosts *string				// from config file and overrides openstack list if given (mostly testing)
		ssq_cmd		*string					// command string used to set switch queues (from config file)

		//max_link_used	int64 = 0			// the current maximum link utilisation
	)

	fq_sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	fq_sheep.Set_prefix( "fq_mgr" )
	tegu_sheep.Add_child( fq_sheep )					// we become a child so that if the master vol is adjusted we'll react too

	// -------------- pick up config file data if there --------------------------------
	if *sdn_host == "" {					// if no sdn host supplied on command line use config file, or default
		if sdn_host = cfg_data["default"]["sdn_host"];  sdn_host == nil {
			sdn_host = &default_sdn
		}
	}

	if cfg_data["fqmgr"] != nil {
		if dp := cfg_data["fqmgr"]["ssq_cmd"]; dp != nil {		// set switch queue command
			ssq_cmd = dp
		} else {
			p := "/opt/app/bin/set_switch_queues"
			ssq_cmd = &p;										// note -- we _can_ take the address of the local var and have it outside of the block!
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
	// ---------------------------------------------------------------------------------

	//tklr.Add_spot( qcheck_freq, my_chan, REQ_SETQUEUES, nil, ipc.FOREVER );  	// tickle us every few seconds to adjust the ovs queues if needed

	if switch_hosts == nil {
		tklr.Add_spot( 2, my_chan, REQ_CHOSTLIST, nil, 1 );  						// tickle once, very soon after starting, to get a host list
		tklr.Add_spot( hcheck_freq, my_chan, REQ_CHOSTLIST, nil, ipc.FOREVER );  	// tickles us every once in a while to update host list
		fq_sheep.Baa( 2, "host list will be requested from openstack every %ds", hcheck_freq )
	} else {
		host_list = switch_hosts
		fq_sheep.Baa( 0, "static host list from config used for setting OVS queues: %s", *host_list )
	}

	if *sdn_host != "" {
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
if spq == nil {
	fq_sheep.Baa( 0, "WRN: spq is nil in IE RESERVE call probably h1 and h2 on same switch" )
	msg.Response_ch = nil
} else {
				if uri_prefix != "" {
					msg.State = gizmos.SK_ie_flowmod( &uri_prefix, data[FQ_IP1].(string), data[FQ_IP2].(string), data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port )

					if msg.State == nil {				// no error, we are silent
						fq_sheep.Baa( 2,  "proactive reserve successfully sent: uri=%s h1=%s h2=%s exp=%d qnum=%d swid=%s port=%d",  
									uri_prefix, data[FQ_IP1].(string), data[FQ_IP2].(string), data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port )
						msg.Response_ch = nil
					} else {
						// do we need to suss out the id and mark it failed, or set a timer on it,  so as not to flood reqmgr with errors?
						fq_sheep.Baa( 1,  "ERR: proactive reserve failed: uri=%s h1=%s h2=%s exp=%d qnum=%d swid=%s port=%d",  
									uri_prefix, data[FQ_IP1].(string), data[FQ_IP2].(string), data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port )
					}
				} else {
					fq_sheep.Baa( 2,  "proactive reservation not sent, no sdn-host defined: uri=%s h1=%s h2=%s exp=%d qnum=%d swid=%s port=%d",  
						uri_prefix, data[FQ_IP1].(string), data[FQ_IP2].(string), data[FQ_EXPIRY].(int64), spq.Queuenum, spq.Switch, spq.Port )
					msg.Response_ch = nil
				}
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
				//req_link_max( my_chan )					// send request to network manager to get the max link utilisation
				qlist := msg.Req_data.( []interface{} )[0].( []string )
				adjust_queues( qlist, ssq_cmd, host_list ) 

			case REQ_CHOSTLIST:								// this is tricky as it comes from tickler as a request, and from openstack as a response, be careful!
				msg.Response_ch = nil;						// regardless of source, we should not reply to this request

				if msg.State != nil || msg.Response_data != nil {				// response from ostack if with list or error
					if  msg.Response_data != nil {
						if  msg.Response_data.( *string ) != nil {
							host_list = msg.Response_data.( *string )
							fq_sheep.Baa( 1, "host list received from osif: %s", *host_list )
						} else {
							fq_sheep.Baa( 0, "WRN: no host data from openstack" )
						}
					}
				} else {
					req_hosts( my_chan )					// send a request to osif for a new host list
				}

			// CAUTION:   these are response messages resulting from requests that we sent off
/*
deprecated with specific path queue setting 
			case REQ_GETLMAX:								// this should be a response from netmgr with the current link max
				msg.Response_ch = nil;						// must set this off as it is our channel!!

				if msg.Response_data != nil {				// response data should exist
					mlu := msg.Response_data.( int64 )
					if mlu != max_link_used {
						fq_sheep.Baa( 2, "reset ovs queues needed: from %d to %d", max_link_used, mlu )
						err := adjust_queues( ssq_cmd, host_list, mlu )	
						if err != nil {
							fq_sheep.Baa( 2, "WRN: unable to adjust queues: %s", err )
						} else {
							max_link_used = mlu
						}
					} else {
						fq_sheep.Baa( 2, "no ovs queue change is needed: %d == %d", max_link_used, mlu )
					}
				} else {
					fq_sheep.Baa( 2, "GETLMAX msg received on channel without a respose" )
				}
*/

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
