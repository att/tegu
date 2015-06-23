// vi: sw=4 ts=4:

/*

	Mnemonic:	tegu_agent
	Abstract:	An agent that connects to tegu and receives requests to act on.

				Command line flags:
					-h host:port -- tegu host an port (default localhost:29055)
					-i id	     -- ID number for this agent
					-k key	     -- ssh key file for the ssh broker
					-l directory -- logfile directory
					-p n		 -- number of parallel ssh to run (default 10)
					-no-rsync    -- turn off rsync feature
					-rdir dir    -- rsync remote directory
					-rlist list  -- list of files to sync to remote hosts
					-u user      -- ssh username to use
					-v			 -- verbose mode
					-V level     -- verbosity level

	Date:		30 April 2014
	Author:		E. Scott Daniels

	Mods:		05 May 2014 : Added ability to support the map_mac2phost request
					which generaets data back to tegu.
				06 May 2014 : Added support to drive setup_ovs_intermed script.
				13 Jun 2014 : Corrected typo in warning message.
				29 Sep 2014 : Better error messages from (some) scripts.
				05 Oct 2014 : Now writes stderr from all commands even if good return.
				14 Jan 2014 : Added ssh-broker support. (bump to 2.0)
				25 Feb 2015 : Added mirroring (version => 2.1), command line flags comment, and "mirrirwiz" handling.
				27 Feb 2015 : Allow fmod to be sent to multiple hosts (steering).
				20 Mar 2015 : Added support for bandwidth flow-mod generation script.
				09 Apr 2015 : Added ql_set_trunks to list of scripts to rsync.
				20 Apr 2015 : Now accepts direction of external IP to pass on bw-fmod command.
				28 May 2015 : Changes to support trinity. (version bump to 2.2)
				15 Jun 2015 : Added support for oneway bandwidth reservations (bump 2.3)
				19 Jun 2015 : Added bwow script to copy list.
				23 Jun 2015 : Removed one result channel close (defer) that was causing a panic in ssh_broker
					if the agent pops the timeout.

	NOTE:		There are three types of generic error/warning messages which have 
				the same message IDs (007, 008, 009) and thus are generated through
				dedicated functions rather than direct calls to Baa().
*/

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"codecloud.web.att.com/gopkgs/bleater"
	"codecloud.web.att.com/gopkgs/connman"
	"codecloud.web.att.com/gopkgs/jsontools"
	"codecloud.web.att.com/gopkgs/ssh_broker"
	"codecloud.web.att.com/gopkgs/token"
)

// globals
var (
	version		string = "v2.3/16155"
	sheep *bleater.Bleater
	shell_cmd	string = "/bin/ksh"

	running_sim	bool = false	// prevent queueing more if one is running (set up intermediate)
	running_map bool = false	// map phost
)


/*
	Structures used to unpack json. These provide a generic
	struture set into which all types of requests can be unpacked.
*/
type json_action struct {
	Atype	string				// action type e.g. intermed_queues, flowmod, etc.
	Aid		uint32				// action id to be sent in the response
	Data	map[string]string	// generic data - probably json directly from the outside world, but who knows
	Qdata	[]string			// queue parms 
	Fdata	[]string			// flow-mod parms
	Hosts	[]string			// hosts to execute on if a multihost command
	Dscps	string				// space separated list of dscp values
}

type json_request struct {
	Ctype	string
	Actions	[]json_action
}

/*
	Structure of message to send to tegu
*/
type agent_msg struct {
	Ctype	string			// command type -- should be response, ack, nack etc.
	Rtype	string			// type of response (e.g. map_mac2phost, or specific id for ack/nack)
	Rdata	[]string		// response stdout data
	Edata	[]string		// response error data
	State	int				// if an ack/nack some state information
	Vinfo	string			// agent version info for debugging
	Rid		uint32			// original request id
}
//--- generic message functions ---------------------------------------------------------------------

/* 
	These message functions ensure that the message text is the same regardless of the function that
	needs to generate a message with the given IDs.
*/
func msg_007( host string, cmd string, err error ) {
	sheep.Baa( 0, "ERR: unable to submit command: on %s: %s: %s	[TGUAGN007]", host, cmd, err )
}

func msg_008( count int ) {
	sheep.Baa( 1, "WRN: timeout waiting for mac2phost responses; %d replies not received   [TGUAGN008]", count )
}

func msg_009( cname string, host string ) {
	sheep.Baa( 1, "WRN: error running %s command on %s  [TGUAGN009]", cname, host )
}

//----------------------------------------------------------------------------------------------------

/*
	Establishes a connection with tegu. This blocks until a connection is established
	and tries every few seconds until successful.
*/
func connect2tegu( smgr *connman.Cmgr, host_port *string, data_chan chan *connman.Sess_data ) {

	burble := 0		// limit our complaining to once a minute or so

	for {
		err := smgr.Connect( *host_port, "c0", data_chan )
		if err == nil {
			sheep.Baa( 1, "connection with tegu established: %s", *host_port )
			return
		}

		if burble <= 0 {
			sheep.Baa( 0, "unable to establish a connection with tegu: %s: %s", *host_port, err )
			burble = 12
		}

		time.Sleep( 5 * time.Duration( time.Second ) )
		burble--
	}
}


/*
	Dumps the bytes buffer a line at a time to our real stdout device.
*/
func dump_stderr( stderr bytes.Buffer, prefix string ) {
		for {												// read until the first error which we assume is io.EOF
			line, err := stderr.ReadBytes( '\n' )
			if err == nil {
				sheep.Baa( 0, "%s stderr:  %s", prefix, bytes.TrimRight( line, "\n" ) )
			} else {
				return
			}
		}
}

/*
	Accept an array and a bytes buffer; save the newline separated records in buf into the array starting at
	the index (sidx). Returns the index
*/
func  buf_into_array( buf bytes.Buffer, a []string, sidx int ) ( idx int ) {
		idx = sidx
		for {												// read until the first error which we assume is io.EOF
			line, err := buf.ReadBytes( '\n' )
			if err == nil {
				if idx < len( a ) {
					a[idx] = string( line[0:len( line )-1] )	// remove trailing newline
					idx++
				}
			} else {
				return
			}
		}
}

// --------------- request support (command execution) ----------------------------------------------------------

/*
	Builds an option string of the form '-X value' if value passed in is not nil, and an empty string if 
	if the value is nil.  If the value is "true" or "True", then '-X' is returned, if value is "false"
	or "False", then an empty string is returned.  Opt is the -X or --longname option to use. If
	opt ends in an equal sign, (e.g. --longname=), then no space will separate the key and value
	in the resulting string.  If opt is empty (""), then that results in just the value being placed
	in the return string such that -x could be sent in the map, or positional paramters sussed out 
	this way too.
*/

func build_opt( value string, opt string ) ( parm string ) {
	parm = ""

	if value == "" {
		return
	}

	if opt == "" {					// value is assumed to be -x or someething of the sort that can stand alone; just return it
		return value + " "
	}

	have_eq := false
	fmt_str := "%s %s "				// default to -x value
	li := len(opt) 					// last index 
	if opt[li-1:li] == "=" {		// --longopt=  we assume, so tokens have no space
		fmt_str = "%s%s "
		have_eq = true
	} 

	switch( value ) {
		case "True", "true", "TRUE":
			if have_eq {
				parm = fmt.Sprintf( fmt_str, opt, value )		// --foo=true
			} else {
				parm = opt + " "								// just -f
			}

		case "False", "false", "FALSE":
			if have_eq {								// need to output --foo=false in this case
				parm = fmt.Sprintf( fmt_str, opt, value )
			}

		default:
			parm = fmt.Sprintf( fmt_str, opt, value )
		
	}
	
	return
	
}

/*	
	Bandwidth flow-mod generation rolls the creation of a set of flow-mods into a single script which 
	eliminates the need for Tegu to understand/know things like command line parms, bridge names and
	such.  Parms in the map are converted to script command line options.
 */
func (act *json_action ) do_bw_fmod( cmd_type string, broker *ssh_broker.Broker, path *string, timeout time.Duration ) ( jout []byte, err error ) {
    var (
		cmd_str string  
    )
	
	pstr := ""
	if path != nil {
		pstr = fmt.Sprintf( "PATH=%s:$PATH ", *path )		// path to add if needed
	}

	parms := act.Data

	cmd_str = fmt.Sprintf( `%sql_bw_fmods `, pstr ) +
			build_opt( parms["smac"], "-s" ) +
			build_opt( parms["dmac"], "-d" ) +
			build_opt( parms["extip"], "-E" ) +
			build_opt( parms["extdir"], "" ) +
			build_opt( parms["vlan_match"],  "-V" ) +
			build_opt( parms["vlan_action"],  "-v" ) +
			build_opt( parms["koe"],  "-k" ) +
			build_opt( parms["sproto"],  "-p" ) +
			build_opt( parms["dproto"],  "-P" ) +
			build_opt( parms["queue"],  "-q" ) +
			build_opt( parms["timeout"],  "-t" ) +
			build_opt( parms["dscp"],  "-T" ) +
			build_opt( parms["oneswitch"], "-o" )  +
			build_opt( parms["ipv6"], "-6" ) 


	sheep.Baa( 1, "via broker on %s: %s", act.Hosts[0], cmd_str )

	msg := agent_msg{}				// build response to send back
	msg.Ctype = "response"
	msg.Rtype = cmd_type
	msg.Rid = act.Aid				// response id so tegu can map back to requestor
	msg.Vinfo = version
	msg.State = 0					// assume success

	ssh_rch := make( chan *ssh_broker.Broker_msg, 256 )					// channel for ssh results
																		// do NOT close the channel here; only senders should close

	err = broker.NBRun_cmd( act.Hosts[0], cmd_str, 0, ssh_rch )			// for now, there will only ever be one host for these commands
	if err != nil {
		sheep.Baa( 1, "WRN: error submitting bandwidth command  to %s: %s", act.Hosts[0], err )
		jout, _ = json.Marshal( msg )
		return
	}

	rdata := make( []string, 8192 )								// response output converted to strings
	edata := make( []string, 8192 )
	ridx := 0													// index of next insert point into rdata
	wait4 := 1
	timer_pop := false
	for wait4 > 0 && !timer_pop {								// wait for response back on the channel or the timer to pop
		select {
			case <- time.After( timeout * time.Second ):		// timeout if we don't get something back soonish
				sheep.Baa( 1, "WRN: timeout waiting for response from %s; cmd: %s", act.Hosts[0], cmd_str )
				timer_pop = true

			case resp := <- ssh_rch:					// response from broker
				wait4--
				stdout, stderr, _, err := resp.Get_results()
				host, _, _ := resp.Get_info()
				eidx := buf_into_array( stderr, edata, 0 )			// capture error messages, if any
				msg.Edata = edata[0:eidx]
				if err != nil {
					msg.State = 1
					sheep.Baa( 1, "WRN: error running command: host=%s: %s", host, err )
				} else {
					ridx = buf_into_array( stdout, rdata, ridx )			// capture what came back for return
				}
				if err != nil || sheep.Would_baa( 2 ) {
					dump_stderr( stderr, "bw_fmod " + host )			// always dump stderr on error, or in chatty mode
				}
		}
	}

	msg.Rdata = rdata[0:ridx]										// return just what was filled in

	if msg.State > 0 {
		sheep.Baa( 1, "bw_fmod (%s) failed: stdout: %d lines;  stderr: %d lines", cmd_type, len( msg.Rdata ), len( msg.Edata )  )
		sheep.Baa( 0, "ERR: %s unable to execute: %s	[TGUAGN000]", cmd_type, cmd_str )
	} else {
		sheep.Baa( 1, "bw_fmod cmd (%s) completed: stdout: %d lines;  stderr: %d lines", cmd_type, len( msg.Rdata ), len( msg.Edata )  )
	}

	jout, err = json.Marshal( msg )
	return
}

/*	
	Oneway bandwidth flow-mod generation rolls the creation of a set of flow-mods into a single script which 
	eliminates the need for Tegu to understand/know things like command line parms, bridge names and
	such.  Parms in the map are converted to script command line options.
 */
func (act *json_action ) do_bwow_fmod( cmd_type string, broker *ssh_broker.Broker, path *string, timeout time.Duration ) ( jout []byte, err error ) {
    var (
		cmd_str string  
    )
	
	pstr := ""
	if path != nil {
		pstr = fmt.Sprintf( "PATH=%s:$PATH ", *path )		// path to add if needed
	}

	parms := act.Data
	cmd_str = fmt.Sprintf( `%sql_bwow_fmods `, pstr ) +
			build_opt( parms["smac"], "-s" ) +
			build_opt( parms["dmac"], "-d" ) +
			build_opt( parms["extip"], "-E" ) +
			build_opt( parms["sproto"],  "-p" ) +
			build_opt( parms["dproto"],  "-P" ) +
			build_opt( parms["queue"],  "-q" ) +
			build_opt( parms["timeout"],  "-t" ) +
			build_opt( parms["dscp"],  "-T" ) +
			build_opt( parms["vlan_match"],  "-V" ) +
			build_opt( parms["ipv6"], "-6" ) 


	sheep.Baa( 1, "via broker on %s: %s", act.Hosts[0], cmd_str )

	msg := agent_msg{}				// build response to send back
	msg.Ctype = "response"
	msg.Rtype = cmd_type
	msg.Rid = act.Aid				// response id so tegu can map back to requestor
	msg.Vinfo = version
	msg.State = 0					// assume success

	ssh_rch := make( chan *ssh_broker.Broker_msg, 256 )					// channel for ssh results
																		// do NOT close the channel here; only senders should close
	err = broker.NBRun_cmd( act.Hosts[0], cmd_str, 0, ssh_rch )			// oneway fmods are only ever applied to one host so [0] is ok
	if err != nil {
		sheep.Baa( 1, "WRN: error submitting bwow command  to %s: %s", act.Hosts[0], err )
		jout, _ = json.Marshal( msg )
		return
	}

	// TODO: this can be moved to a function 
	rdata := make( []string, 8192 )								// response output converted to strings
	edata := make( []string, 8192 )
	ridx := 0													// index of next insert point into rdata
	wait4 := 1
	timer_pop := false
	for wait4 > 0 && !timer_pop {								// wait for response back on the channel or the timer to pop
		select {
			case <- time.After( timeout * time.Second ):		// timeout if we don't get something back soonish
				sheep.Baa( 1, "WRN: timeout waiting for response from %s; cmd: %s", act.Hosts[0], cmd_str )
				timer_pop = true

			case resp := <- ssh_rch:					// response from broker
				wait4--
				stdout, stderr, _, err := resp.Get_results()
				host, _, _ := resp.Get_info()
				eidx := buf_into_array( stderr, edata, 0 )			// capture error messages, if any
				msg.Edata = edata[0:eidx]
				if err != nil {
					msg.State = 1
					sheep.Baa( 1, "WRN: error running command: host=%s: %s", host, err )
				} else {
					ridx = buf_into_array( stdout, rdata, ridx )			// capture what came back for return
				}
				if err != nil || sheep.Would_baa( 2 ) {
					dump_stderr( stderr, "bw_fmod " + host )			// always dump stderr on error, or in chatty mode
				}
		}
	}

	msg.Rdata = rdata[0:ridx]										// return just what was filled in

	if msg.State > 0 {
		sheep.Baa( 1, "bwow_fmod (%s) failed: stdout: %d lines;  stderr: %d lines", cmd_type, len( msg.Rdata ), len( msg.Edata )  )
		sheep.Baa( 0, "ERR: %s unable to execute: %s	[TGUAGN000]", cmd_type, cmd_str )
	} else {
		sheep.Baa( 1, "bwow_fmod cmd (%s) completed: stdout: %d lines;  stderr: %d lines", cmd_type, len( msg.Rdata ), len( msg.Edata )  )
	}

	jout, err = json.Marshal( msg )
	return
}

/*
	Generate a map that lists physical host and mac addresses. Timeout is the max number of 
	seconds that we will wait for all responses.  If timeout seconds passes before all 
	responses are received we will return what we have. The map command is executed on all
	hosts, so we send a non-blocking command to the broker for each host and wait for the
	responses to come back on the channel.  This allows them to run in parallel across
	the cluster.
*/
func do_map_mac2phost( req json_action, broker *ssh_broker.Broker, path *string, timeout time.Duration ) ( jout []byte, err error ) {
    var (
		cmd_str string
    )

	startt := time.Now().Unix()

	ssh_rch := make( chan *ssh_broker.Broker_msg, 256 )		// channel for ssh results
															// do NOT close this channel, only senders should close

	wait4 := 0											// number of responses to wait for
	for k, v := range req.Hosts {						// submit them all out non-blocking
		cmd_str = fmt.Sprintf( "PATH=%s:$PATH map_mac2phost -p %s localhost", *path, v )
		err := broker.NBRun_cmd( req.Hosts[k], cmd_str, wait4, ssh_rch )
		if err != nil {
			msg_007( req.Hosts[k], cmd_str, err )
		} else {
			wait4++
		}
	}

	msg := agent_msg{}									// message to return
	msg.Ctype = "response"
	msg.Rtype = "map_mac2phost"
	msg.Vinfo = version
	msg.State = 0

	rdata := make( []string, 8192 )		// might need to revisit this limit
	ridx := 0

	sheep.Baa( 2, "map_mac2phost: waiting for %d responses", wait4 )
	timer_pop := false						// indicates a timeout for loop exit
	errcount := 0
	for wait4 > 0 && !timer_pop {			// wait for responses back on the channel or the timer to pop
		select {
			case <- time.After( timeout * time.Second ):		// timeout after 15 seconds
				msg_008( wait4 )
				timer_pop = true

			case resp := <- ssh_rch:					// response from broker
				wait4--
				stdout, stderr, elapsed, err := resp.Get_results()
				host, _, _ := resp.Get_info()
				sheep.Baa( 2, "map_mac2phost: received response from %s elap=%d err=%v, waiting for %d more", host, elapsed, err != nil, wait4 )
				if err != nil {
					msg_009( "map_mac2phost", host )
					errcount++
				} else {
					ridx = buf_into_array( stdout, rdata, ridx )			// capture what came back for return
				}
				if err != nil || sheep.Would_baa( 2 ) {
					dump_stderr( stderr, "map_mac2phost" + host )			// always dump stderr on error, or in chatty mode
				}
		}
	}

	msg.Rdata = rdata[0:ridx]										// return just what was filled in
	endt := time.Now().Unix()
	sheep.Baa( 1, "map-mac2phost: timeout=%v %ds elapsed for %d hosts %d errors %d elements", timer_pop, endt - startt, len( req.Hosts ), errcount, len( msg.Rdata ) )

	jout, err = json.Marshal( msg )
	return
}

/*
	Executes the setup_ovs_intermed script on each host listed. This command can take
	a significant amount of time on each host (10s of seconds) and so we submit the
	command to the broker for each host in non-blocking mode to allow them to
	run concurrently. Once submitted, we collect the results (reporting errors)
	as the broker writes the response back on the channel.
*/
func do_intermedq( req json_action, broker *ssh_broker.Broker, path *string, timeout time.Duration ) {

	startt := time.Now().Unix()

	running_sim = true										// prevent queuing another of these
	sheep.Baa( 1, "running intermediate switch queue/fmod setup on all hosts (broker)" )

	ssh_rch := make( chan *ssh_broker.Broker_msg, 256 )		// channel for ssh results
															// do NOT close the channel here; only senders should close

	wait4 := 0												// number of responses to wait for
	for i := range req.Hosts {
		cmd_str := fmt.Sprintf( `PATH=%s:$PATH setup_ovs_intermed -d "%s"`, *path, req.Dscps )
    	sheep.Baa( 1, "via broker on %s: %s", req.Hosts[i], cmd_str )

		err := broker.NBRun_cmd( req.Hosts[i], cmd_str, wait4, ssh_rch )
		if err != nil {
			msg_007( req.Hosts[i], cmd_str, err )
		} else {
			wait4++
		}
	}

	timer_pop := false
	errcount := 0
	for wait4 > 0 && !timer_pop {							// collect responses logging any errors
		select {
			case <- time.After( timeout * time.Second ):		// timeout
				msg_008( wait4 )
				timer_pop = true

			case resp := <- ssh_rch:							// response back from the broker
				wait4--
				_, stderr, elapsed, err := resp.Get_results()
				host, _, _ := resp.Get_info()
				sheep.Baa( 2, "setup-intermed: received response from %s elap=%d err=%v, waiting for %d more", host, elapsed, err != nil, wait4 )
				if err != nil {
					msg_009( "setup_intermed", host )
					errcount++
				}
				if err != nil || sheep.Would_baa( 2 ) {
					dump_stderr( stderr, "setup-intermed" + host )			// always dump on error, or if chatty
				}
		}
	}

	endt := time.Now().Unix()
	sheep.Baa( 1, "setup-intermed: timeout=%v %ds elapsed for %d hosts %d errors", timer_pop, endt - startt, len( req.Hosts ), errcount )
	running_sim = false
}

/*
	Execute a create_ovs_queues for each host in the list. The create queues script is unique inasmuch
	as it expects an input file that is supplied either as a filename as $1, or on stdin if $1 is omitted.
	To send the data file for the command to execute, we'll create a tmp file on the local machine which
	is a script that echos the data into the script:
		cat <<endKat | create_ovs_queues
			data passed to us
		endKat

	We'll use the brokers 'send script for execution' feature rather to execute our script.
*/
func do_setqueues( req json_action, broker *ssh_broker.Broker, path *string, timeout time.Duration ) {
    var (
        err error
    )

	startt := time.Now().Unix()

    fname := fmt.Sprintf( "/tmp/tegu_setq_%d_%x_%02d.data", os.Getpid(), time.Now().Unix(), rand.Intn( 10 ) )
    sheep.Baa( 3, "adjusting queues: creating %s will contain %d items", fname, len( req.Qdata ) );

    f, err := os.Create( fname )
    if err != nil {
        sheep.Baa( 0, "ERR: unable to create data file: %s: %s	[TGUAGN002]", fname, err )
        return
    }

	fmt.Fprintf( f, "#!/usr/bin/env ksh\ncat <<endKat | PATH=%s:$PATH create_ovs_queues\n", *path )
    for i := range req.Qdata {
        sheep.Baa( 3, "writing queue info: %s", req.Qdata[i] )
        fmt.Fprintf( f, "%s\n", req.Qdata[i] )
    }
	fmt.Fprintf( f, "endKat\n" )

    err = f.Close( )
    if err != nil {
        sheep.Baa( 0, "ERR: unable to create data file (close): %s: %s	[TGUAGN003]", fname, err )
        return
    }

	ssh_rch := make( chan *ssh_broker.Broker_msg, 256 )		// channel for ssh results
															// do NOT close the channel here; only senders should close

	wait4 := 0												// number of responses to wait for
	for i := range req.Hosts {
    	sheep.Baa( 1, "via broker on %s: create_ovs_queues embedded in %s", req.Hosts[i], fname )

		err := broker.NBRun_on_host( req.Hosts[i], fname, "", wait4, ssh_rch )		// sends the file as input to be executed on the host
		if err != nil {
			msg_007( req.Hosts[i], "create_ovs_queues", err )
		} else {
			wait4++
		}
	}

	timer_pop := false
	errcount := 0
	for wait4 > 0 && !timer_pop {							// collect responses logging any errors
		select {
			case <- time.After( timeout * time.Second ):		// timeout
				msg_008( wait4 )
				timer_pop = true

			case resp := <- ssh_rch:							// response back from the broker
				wait4--
				_, stderr, elapsed, err := resp.Get_results()
				host, _, _ := resp.Get_info()
				sheep.Baa( 2, "create-q: received response from %s elap=%d err=%v, waiting for %d more", host, elapsed, err != nil, wait4 )
				if err != nil {
        			sheep.Baa( 0, "ERR: unable to execute set queue command on %s: data=%s: %s  [TGUAGN004]", host, fname, err )
					errcount++
				}  else {
        			sheep.Baa( 1, "queues adjusted succesfully on: %s", host )
				}
				if err != nil || sheep.Would_baa( 2 ) {
					dump_stderr( stderr, "create-q" + host )			// always dump on error, or if chatty
				}
		}
	}

	endt := time.Now().Unix()
	sheep.Baa( 1, "create-q: timeout=%v %ds elapsed %d hosts %d errors", timer_pop, endt - startt, len( req.Hosts ), errcount )

	if errcount == 0 {							// ditch the script we built earlier if all successful
		os.Remove( fname )
	} else {
		sheep.Baa( 1, "create-q: %d errors, generated script file kept: %s", fname )
	}

}

/*
	Extracts the information from the action passed in and causes the fmod command
	to be executed.
*/
func do_fmod( req json_action, broker *ssh_broker.Broker, path *string, timeout time.Duration ) ( err error ){

	startt := time.Now().Unix()

	errcount := 0
/*
	for i := range req.Fdata {
	for hi := range req.Hosts {			//TODO:  send them in parallel

		cstr := fmt.Sprintf( `PATH=%s:$PATH send_ovs_fmod %s`, *path, req.Fdata[i] )
    	sheep.Baa( 1, "via broker on %s: %s", req.Hosts[0], cstr )

		_, stderr, err := broker.Run_cmd( req.Hosts[hi], cstr )				// there is at most only one host when sending fmods
		if err != nil {
			sheep.Baa( 0, "ERR: send fmod failed host=%s: %s	[TGUAGN005]", req.Hosts[hi], err )
			errcount++
		} else {
        	sheep.Baa( 2, "fmod succesfully sent: %s", cstr )
		}
		for {
			line, err := stderr.ReadBytes( '\n' )
			if err == nil {
				sheep.Baa( 0, "send_fmod stderr:  %s", bytes.TrimRight( line, "\n" ) )
			} else {
				break
			}
		}
	}
	}

*/
	for f := range req.Fdata {
		cstr := fmt.Sprintf( `PATH=%s:$PATH send_ovs_fmod %s`, *path, req.Fdata[f] )

		ssh_rch := make( chan *ssh_broker.Broker_msg, 256 )		// channel for ssh results
																// do NOT close the channel here; only senders should close

		wait4 := 0												// number of responses to wait for
		for i := range req.Hosts {
			sheep.Baa( 1, "via broker on %s send fmod: %s", req.Hosts[i], cstr )

			err := broker.NBRun_cmd( req.Hosts[i], cstr, wait4, ssh_rch )		// sends the file as input to be executed on the host
			if err != nil {
				msg_007( req.Hosts[i], cstr, err )
			} else {
				wait4++
			}
		}

		timer_pop := false
		errcount := 0
		for wait4 > 0 && !timer_pop {							// collect responses logging any errors
			select {
				case <- time.After( timeout * time.Second ):		// timeout
					msg_008( wait4 )
					timer_pop = true

				case resp := <- ssh_rch:							// response back from the broker
					wait4--
					_, stderr, elapsed, err := resp.Get_results()
					host, _, _ := resp.Get_info()
					sheep.Baa( 1, "send-fmod: received response from %s elap=%d err=%v, waiting for %d more", host, elapsed, err != nil, wait4 )
					if err != nil {
						sheep.Baa( 0, "ERR: unable to execute send-fmod command on %s: data=%s  %s	[TGUAGN004]", host, cstr, err )  
						errcount++
					}  else {
						sheep.Baa( 1, "flow mod set on: %s", host )
					}
					if err != nil || sheep.Would_baa( 2 ) {
						dump_stderr( stderr, "send-fmod" + host )			// always dump on error, or if chatty
					}
			}
		}
	}

	endt := time.Now().Unix()
	sheep.Baa( 1, "fmod: %ds elapsed %d fmods %d errors", endt - startt, len( req.Fdata ),  errcount )

	return
}

/*
 *  Invoke the tegu_add_mirror or tegu_del_mirror command on a remote host in order to add/remove a mirror.
 */
func do_mirrorwiz( req json_action, broker *ssh_broker.Broker, path *string ) {
	startt := time.Now().UnixNano()

	cstr := ""
	switch (req.Qdata[0]) {
		case "add":
			cstr = fmt.Sprintf( `PATH=%s:$PATH tegu_add_mirror %s %s %s`, *path, req.Qdata[1], req.Qdata[2], req.Qdata[3] )
			if len(req.Qdata) > 4 {
				// If VLAN list is in the arguments, tack that on the end
				cstr += " " + req.Qdata[4]
			}

		case "del":
			cstr = fmt.Sprintf( `PATH=%s:$PATH tegu_del_mirror %s`, *path, req.Qdata[1] )
	}
	if cstr != "" {
    	sheep.Baa( 1, "via broker on %s: %s", req.Hosts[0], cstr )
		_, _, err := broker.Run_cmd( req.Hosts[0], cstr )
		if err != nil {
			sheep.Baa( 0, "ERR: send mirror cmd failed host=%s: %s	[TGUAGN005]", req.Hosts[0], err )
		} else {
        	sheep.Baa( 2, "mirror cmd succesfully sent: %s", cstr )
		}
	} else {
		sheep.Baa( 0, "Unrecognized mirror command: " + req.Qdata[0] )
	}
	endt := time.Now().UnixNano()
	sheep.Baa( 1, "do_mirrorwiz: %d ms elapsed", (endt - startt) / 1000 )
}

/*
	Unpacks the json blob into the generic json request structure and validates that the ctype
	is one of the epected types.  The only supported ctype at the moment is action_list; this
	function will then split out the actions and invoke the proper do_* function to
	exeute the action.

	Returns a list of responses that should be written back to tegu, or nil if none of the
	requests produced responses.
*/
func handle_blob( jblob []byte, broker *ssh_broker.Broker, path *string ) ( resp [][]byte ) {
	var (
		req	json_request		// unpacked request struct
		ridx int = 0
	)

	resp = make( [][]byte, 128 )

    err := json.Unmarshal( jblob, &req )           // unpack the json
	if err != nil {
		sheep.Baa( 0, "ERR: unable to unpack request: %s	[TGUAGN006]", err )
		sheep.Baa( 0, "got: %s", jblob )
		return
	}

	if req.Ctype != "action_list" {
		sheep.Baa( 0, "unknown request type received from tegu: %s", req.Ctype )
		return
	}

	for i := range req.Actions {
		switch( req.Actions[i].Atype ) {
			case "setqueues":								// set queues
					do_setqueues( req.Actions[i], broker, path, 30 )

			case "flowmod":									// set a flow mod
					do_fmod( req.Actions[i], broker, path, 30 )

			case "map_mac2phost":							// run script to generate mac to physical host mappings
					p, err := do_map_mac2phost( req.Actions[i], broker, path, 15 )
					if err == nil {
						resp[ridx] = p
						ridx++
					}

			case "intermed_queues":													// setup intermediate queues
					if ! running_sim {												// it's not good to start overlapping setup scripts
						go do_intermedq(  req.Actions[i], broker, path, 3600 )		// this can run asynch since there isn't any output
					} else {
						sheep.Baa( 1, "handle blob: setqueues still running, not restarted" )
					}

			case "mirrorwiz":
					do_mirrorwiz(req.Actions[i], broker, path)

			case "bw_fmod":									// new bandwidth flow-mod
					p, err := req.Actions[i].do_bw_fmod( req.Actions[i].Atype, broker, path, 15 )
					if err == nil {
						resp[ridx] = p
						ridx++
					}

			case "bwow_fmod":									// generate oneway bandwidth flow-mods
					p, err := req.Actions[i].do_bwow_fmod( req.Actions[i].Atype, broker, path, 15 )
					if err == nil {
						resp[ridx] = p
						ridx++
					}

			default:
				sheep.Baa( 0, "unknown action type received from tegu: %s", req.Actions[i].Atype )
		}
	}

	if ridx > 0 {
		resp = resp[0:ridx]
	} else {
		resp = nil
	}

	return
}


func usage( version string ) {
	fmt.Fprintf( os.Stdout, "tegu_agent %s\n", version )
	fmt.Fprintf( os.Stdout, "usage: tegu_agent -i id [-h host:port] [-l log-dir] [-p n] [-v | -V level] [-k key] [-no-rsync] [-rdir dir] [-rlist list] [-u user]\n" )
}

func main() {

	home := os.Getenv( "HOME" )
	def_user := os.Getenv( "LOGNAME" )
	def_rdir := "/tmp/tegu_b"					// rsync directory created on remote hosts
	def_rlist := 								// list of scripts to copy to remote hosts for execution
			"/usr/bin/create_ovs_queues " +
			"/usr/bin/map_mac2phost " +
			"/usr/bin/ovs_sp2uuid " +
			"/usr/bin/purge_ovs_queues " +
			"/usr/bin/ql_setup_irl " +
			"/usr/bin/send_ovs_fmod " +
			"/usr/bin/tegu_add_mirror " +
			"/usr/bin/tegu_del_mirror " +
			"/usr/bin/ql_bw_fmods " +
			"/usr/bin/ql_bwow_fmods " +
			"/usr/bin/ql_set_trunks " +
			"/usr/bin/ql_filter_rtr " +
			"/usr/bin/setup_ovs_intermed "

	if home == "" {
		home = "/home/tegu"					// probably bogus, but we'll have something
	}
	def_key := home + "/.ssh/id_rsa," + home + "/.ssh/id_dsa"		// default ssh key to use

	needs_help := flag.Bool( "?", false, "show usage" )				// define recognised command line options
	id := flag.Int( "i", 0, "id" )
	key_files := flag.String( "k", def_key, "ssh-key file(s) for broker" )
	log_dir := flag.String( "l", "stderr", "log_dir" )
	parallel := flag.Int( "p", 10, "parallel ssh commands" )
	no_rsync := flag.Bool( "no-rsync", false, "turn off rsync" )
	rdir := flag.String( "rdir", def_rdir, "rsync remote directory" )
	rlist := flag.String( "rlist", def_rlist, "rsync file list" )
	tegu_host := flag.String( "h", "localhost:29055", "tegu_host:port" )
	user	:= flag.String( "u", def_user, "ssh user-name" )
	verbose := flag.Bool( "v", false, "verbose" )
	vlevel := flag.Int( "V", 1, "verbose-level" )
	flag.Parse()									// actually parse the commandline

	if *needs_help {
		usage( version )
		os.Exit( 0 )
	}

	if *id <= 0 {
		fmt.Fprintf( os.Stderr, "ERR: must enter -i id (number) on command line\n" )
		os.Exit( 1 )
	}

	sheep = bleater.Mk_bleater( 0, os.Stderr )
	sheep.Set_prefix( fmt.Sprintf( "agent-%d", *id ) )		// append the pid so that if multiple agents are running they'll use different log files

	if *needs_help {
		usage( version )
		os.Exit( 0 )
	}

	if( *verbose ) {
		sheep.Set_level( 1 )
	} else {
		if( *vlevel > 0 ) {
			sheep.Set_level( uint( *vlevel ) )
		}
	}

	if *log_dir  != "stderr" {							// allow it to stay on stderr
		lfn := sheep.Mk_logfile_nm( log_dir, 86400 )
		sheep.Baa( 1, "switching to log file: %s", *lfn )
		sheep.Append_target( *lfn, false )						// switch bleaters to the log file rather than stderr
		go sheep.Sheep_herder( log_dir, 86400 )						// start the function that will roll the log now and again
	}

	sheep.Baa( 1, "tegu_agent %s started", version )
	sheep.Baa( 1, "will contact tegu on port: %s", *tegu_host )

	jc := jsontools.Mk_jsoncache( )							// create json cache to buffer tegu datagram input
	sess_mgr := make( chan *connman.Sess_data, 1024 )		// session management to create tegu connections with and drive the session listener(s)
	smgr := connman.NewManager( "", sess_mgr );				// get a manager, but no listen port opened

	connect2tegu( smgr, tegu_host, sess_mgr )				// establish initial connection

	ntoks, key_toks := token.Tokenise_populated( *key_files, " ," )		// allow space or , seps and drop nil tokens
	if ntoks <= 0 {
		sheep.Baa( 0, "CRI: no ssh key files given (-k)" )
		os.Exit( 1 )
	}
	keys := make( []string, ntoks )
	for i := range key_toks  {
		keys[i] = key_toks[i]
	}
	broker := ssh_broker.Mk_broker( *user,  keys )
	if broker == nil {
		sheep.Baa( 0, "CRI: unable to create an ssh broker" )
		os.Exit( 1 )
	}
	if ! *no_rsync {
		sheep.Baa( 1, "will sync these files to remote hosts: %s", *rlist )
		broker.Add_rsync( rlist, rdir )
	}
	sheep.Baa( 1, "successfully created ssh_broker for user: %s, command path: %s", *user, *rdir )
	broker.Start_initiators( *parallel )


	for {
		select {									// wait on input from any channel -- just one now, but who knows
			case sreq := <- sess_mgr:				// data from the network
				switch( sreq.State ) {
					case connman.ST_ACCEPTED:		// shouldn't happen
						sheep.Baa( 1, "this shouldn't happen; accepted session????" );

					case connman.ST_NEW:			// new connection; nothing to process here

					case connman.ST_DISC:
						sheep.Baa( 1, "session to tegu was lost" )
						connect2tegu( smgr, tegu_host, sess_mgr )			// blocks until connected and reports on the conn_ch channel when done
						broker.Reset( )				// reset the broker each time we pick up a new tegu connection

					case connman.ST_DATA:
						sheep.Baa( 3, "data: [%s]  %d bytes received", sreq.Id, len( sreq.Buf ) )
						jc.Add_bytes( sreq.Buf )
						jblob := jc.Get_blob()		// get next blob if ready
						for ; jblob != nil ; {
							resp := handle_blob( jblob, broker, rdir )
							if resp != nil {
								for i := range resp {
									smgr.Write( sreq.Id, resp[i] )
								}
							}

							jblob = jc.Get_blob()	// get next blob if more than one in the cache
						}
				}
		}			// end select
	}
}
