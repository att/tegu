// vi: sw=4 ts=4:

/*

	Mnemonic:	tegu_agent
	Abstract:	An agent that connects to tegu and receives requests to act on.

	Date:		30 April 2014
	Author:		E. Scott Daniels

	Mods:		05 May 2014 : Added ability to support the map_mac2phost request
					which generaets data back to tegu.
				06 May 2014 : Added support to drive setup_ovs_intermed script.
				13 Jun 2014 : Corrected typo in warning message.
				29 Sep 2014 : Better error messages from (some) scripts.
				05 Oct 2014 : Now writes stderr from all commands even if good return.
				06 Jan 2015 : Wa support.
*/

package main

import (
	//"bufio"
	"encoding/json"
	"flag"
	"fmt"
	//"io/ioutil"
	//"html"
	"math/rand"
	//"net/http"
	"os"
	//"os/exec"
	"strings"
	//"sync"
	"time"

	"codecloud.web.att.com/gopkgs/bleater"
	"codecloud.web.att.com/gopkgs/extcmd"
	"codecloud.web.att.com/gopkgs/connman"
	"codecloud.web.att.com/gopkgs/jsontools"

	//"codecloud.web.att.com/gopkgs/clike"
	//"codecloud.web.att.com/gopkgs/token"
	//"codecloud.web.att.com/gopkgs/ipc"
)

// globals
var (
	version		string = "v1.2/11065"		// wide area support added
	sheep *bleater.Bleater
	shell_cmd	string = "/bin/ksh"
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
	Rid		uint32			// reuest id that this is a response to
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

// --------------- request support (command execution) ----------------------------------------------------------

/*	Run a wide area command which fits a generic profile. The agent (us) is expected to know what values need to be pulled
	from the parm list and how they are placed on the command line. Tegu's agent manager knows what the 
	interface is with the caller (could be WACC, could be something different) and thus tegu is 
	responsible for taking the raw stdout and putting it into a form that the requestor can digest.

	Type is "wa_port", "wa_tunnel", or "wa_route"
 */
func (act *json_action ) do_wa_cmd( cmd_type string ) ( jout []byte, err error ) {
    var (
		cmd_str string  
    )
	
	sheep.Baa( 0, ">>>> running %s", cmd_type )
	parms := act.Data

	//TODO:  need daves command syntax
	switch cmd_type {
		case "wa_port":
				cmd_str = fmt.Sprintf( `echo "result1  result2  result3   miscjunk: host: %s wa_port -t %s -s %s -T %s"`, act.Hosts[0], parms["tenant"], parms["subnet"], parms["token"] )

		case "wa_tunnel":
				cmd_str = fmt.Sprintf( `echo "result1 result2 result3 result4 result5    addWANTunnel router-uuid remote-ip   host: %s wa_tunnel %s %s %s %s %s"`, 
						act.Hosts[0], parms["localtenant"], parms["localrouter"], parms["localip"], parms["remoteip"], parms["bandwidth"] )

		case "wa_route":
				cmd_str = fmt.Sprintf( `echo "host: %s wa_route %s %s %s %s %s %s"`, 
						act.Hosts[0], parms["localtenant"], parms["localrouter"], parms["localip"], parms["remoteip"], parms["remote_cidr"], parms["bandwidth"] )
	}

	sheep.Baa( 1, "wa_cmd executing: %s", cmd_str )

	msg := agent_msg{}
	msg.Ctype = "response"
	msg.Rtype = cmd_type
	msg.Rid = act.Aid				// response id so tegu can map back to requestor
	msg.Vinfo = version
	msg.State = 0
	msg.Rdata, msg.Edata, err = extcmd.Cmd2strings( cmd_str ) 		// execute command and package output as json in response format
	sheep.Baa( 1, "wa_cmd (%s) completed: respone data had %d elements", cmd_type, len( msg.Rdata ) )

	if err != nil {
		msg.State = 1
		sheep.Baa( 0, "ERR: %s unable to execute: %s: %s	[TGUAGN000]", cmd_type, cmd_str, err )
		for i := range msg.Edata {
			sheep.Baa( 0, "stderr: %s", msg.Edata[i] )
		}
		jout = nil
		return
	}

	jout, err = json.Marshal( msg )
	return
}

/*
	Generate a map that lists physical host and mac addresses.
	
*/
func do_map_mac2phost( req json_action ) ( jout []byte, err error ) {
    var (
		cmd_str string  
    )

	cmd_str = strings.Join( req.Hosts, " " )
	cmd_str = "map_mac2phost " + cmd_str

	msg := agent_msg{}
	msg.Ctype = "response"
	msg.Rtype = "map_mac2phost"
	msg.Vinfo = version
	msg.State = 0
	msg.Rdata, msg.Edata, err = extcmd.Cmd2strings( cmd_str ) 		// execute command and package output as a json in response format
	sheep.Baa( 1, "map_mac2phost completed: respone data had %d elements", len( msg.Rdata ) )

	if err != nil {
		msg.State = 1
		sheep.Baa( 0, "ERR: unable to execute: %s: %s	[TGUAGN000]", cmd_str, err )
		for i := range msg.Edata {
			sheep.Baa( 0, "stderr: %s", msg.Edata[i] )
		}
		jout = nil
	}

	jout, err = json.Marshal( msg )
	return
}

/*
	Executes the setup_ovs_intermed script on each host listed.
*/
func do_intermedq( req json_action ) {

	sheep.Baa( 1, "running intermediate switch queue/fmod setup on all hosts" )

	for i := range req.Hosts {
		cmd_str := fmt.Sprintf( `setup_ovs_intermed -h %s -d "%s"`, req.Hosts[i], req.Dscps )

    	sheep.Baa( 2, "executing: %s", cmd_str )

		_, edata, err := extcmd.Cmd2strings( cmd_str ) 		// execute command and package output as a set of strings
		if err != nil {
			sheep.Baa( 0, "ERR: setup_ovs_intermed failed: %s	[TGUAGN001]", err )
		} else {
        	sheep.Baa( 2, "queues adjusted succesfully" )
		}
		for i := range edata {
			sheep.Baa( 0, "set-intermed stderr:  %s", edata[i] )
		}
	}
}

/*
	Execute a create_ovs_queues for each host in the list.
*/
func do_setqueues( req json_action ) {
    var (
        err error
    )

    fname := fmt.Sprintf( "/tmp/tegu_setq_%d_%x_%02d.data", os.Getpid(), time.Now().Unix(), rand.Intn( 10 ) )
    sheep.Baa( 2, "adjusting queues: creating %s will contain %d items", fname, len( req.Qdata ) );

    f, err := os.Create( fname )
    if err != nil {
        sheep.Baa( 0, "ERR: unable to create data file: %s: %s	[TGUAGN002]", fname, err )
        return
    }

    for i := range req.Qdata {
        sheep.Baa( 2, "writing queue info: %s", req.Qdata[i] )
        fmt.Fprintf( f, "%s\n", req.Qdata[i] )
    }

    err = f.Close( )
    if err != nil {
        sheep.Baa( 0, "ERR: unable to create data file (close): %s: %s	[TGUAGN003]", fname, err )
        return
    }

	for i := range req.Hosts {
		cmd_str := fmt.Sprintf( `%s create_ovs_queues -h %s %s`, shell_cmd, req.Hosts[i], fname )
    	sheep.Baa( 1, "executing: %s", cmd_str )

		_, edata, err := extcmd.Cmd2strings( cmd_str ) 										// execute command and package output as a set of strings
    	if err != nil  {
        	sheep.Baa( 0, "ERR: unable to execute set queue command on %s: data=%s:  %s	[TGUAGN004]", req.Hosts[i], fname, err )
    	} else {
        	sheep.Baa( 1, "queues adjusted succesfully on: %s", req.Hosts[i] )
    	}
		for i := range edata {																// always put out stderr
			sheep.Baa( 0, "create queues stderr:  %s", edata[i] )
		}
	}
}

/*
	Extracts the information from the action passed in and causes the fmod command
	to be executed.   We must build the command by hand because the Command() function
	doesn't properly handle quotes. 
*/
func do_fmod( req json_action ) ( err error ){

	for i := range req.Fdata {
		cstr := fmt.Sprintf( "%s send_ovs_fmod %s", shell_cmd, req.Fdata[i] )
    	sheep.Baa( 1, "executing: %s", cstr )

		_, edata, err := extcmd.Cmd2strings( cstr )
		if err != nil {
			sheep.Baa( 0, "ERR: send fmod failed: %s	[TGUAGN005]", err )
		} else {
        	sheep.Baa( 2, "fmod succesfully sent" )
		}
		for i := range edata {
			sheep.Baa( 1, "fmod stderr:  %s", edata[i] )
		}
	}

	return
}

/*
	Unpacks the json blob into the generic json request structure and validates that the ctype
	is one of the epected types.  The only supported ctype at the moment is action_list; this
	function will then split out the actions and invoke the proper do_* function to 
	exeute the action.

	Returns a list of responses that should be written back to tegu, or nil if none of the 
	requests produced responses.
*/
func handle_blob( jblob []byte ) ( resp [][]byte ) {
	var (
		req	json_request		// unpacked request struct
		ridx int = 0
	)

	resp = make( [][]byte, 128 )

    err := json.Unmarshal( jblob, &req )           // unpack the json 
	if err != nil {
		sheep.Baa( 0, "ERR: unable to unpack request: %s	[TGUAGN006]", err )
		return
	}

	if req.Ctype != "action_list" {
		sheep.Baa( 0, "WRN: unknown request type received from tegu: %s", req.Ctype )
		return
	}

	for i := range req.Actions {
		switch( req.Actions[i].Atype ) {
			case "setqueues":								// set queues
					do_setqueues( req.Actions[i] )

			case "flowmod":									// set a flow mod
					do_fmod( req.Actions[i] )

			case "map_mac2phost":							// run script to generate mac to physical host mappings 
					p, err := do_map_mac2phost( req.Actions[i] )
					if err == nil {
						resp[ridx] = p
						ridx++
					}

			case "intermed_queues":							// run script to set up intermediate queues
					do_intermedq(  req.Actions[i] )

			case "wa_port", "wa_tunnel", "wa_route":
					p, err := req.Actions[i].do_wa_cmd( req.Actions[i].Atype )
					if err == nil {
						resp[ridx] = p
						ridx++
					}

			default:
				sheep.Baa( 0, "WRN: unknown action type received from tegu: %s", req.Actions[i].Atype )
		}
	}

	if ridx > 0 {
				sheep.Baa( 0, ">>>> returning %d responses", ridx )
		resp = resp[0:ridx]
	} else {
		resp = nil
	}

	return
}


func usage( version string ) {
	fmt.Fprintf( os.Stdout, "tegu_agent %s\n", version )
	fmt.Fprintf( os.Stdout, "usage: tegu_agent -i id [-l log-dir] [-p tegu-port] [-v | -V level]\n" )
}

func main() {
	var (
		//jc			*jsontools.Jsoncache		// where we stash input until a complete blob is read
	)

	needs_help := flag.Bool( "?", false, "show usage" )
	id := 		flag.Int( "i", 0, "id" )
	log_dir :=	flag.String( "l", "stderr", "log_dir" )
	tegu_host := flag.String( "h", "localhost:29055", "tegu_host:port" )
	verbose :=	flag.Bool( "v", false, "verbose" )
	vlevel :=	flag.Int( "V", 1, "verbose-level" )
	flag.Parse()									// actually parse the commandline

	if *needs_help {
		usage( version )
		os.Exit( 0 )
	}

	if *id <= 0 {
		fmt.Fprintf( os.Stderr, "ERR: must enter -i id (number) on command line\n" )
		os.Exit( 1 )
	}

	sheep = bleater.Mk_bleater( 1, os.Stderr )
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

	for {
		select {									// wait on input from any channel -- just one now, but who knows
			case sreq := <- sess_mgr:				// data from the network
				switch( sreq.State ) {
					case connman.ST_ACCEPTED:		// shouldn't happen
						sheep.Baa( 1, "WRN: this shouldn't happen; accepted session????" );

					case connman.ST_NEW:			// new connection; nothing to process here
				
					case connman.ST_DISC:
						sheep.Baa( 1, "WRN: session to tegu was lost" )
						connect2tegu( smgr, tegu_host, sess_mgr )
						
					case connman.ST_DATA:
						sheep.Baa( 2, "data: [%s]  %d bytes received", sreq.Id, len( sreq.Buf ) )
						jc.Add_bytes( sreq.Buf )
						jblob := jc.Get_blob()		// get next blob if ready
						for ; jblob != nil ; {
							resp := handle_blob( jblob )
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

	os.Exit( 0 )
}

