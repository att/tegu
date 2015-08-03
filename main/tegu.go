//vi: sw=4 ts=4:
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

	Mnemonic:	tegu
	Abstract:	The middle layer that sits between the cQoS and the openflow controller (Skoogi)
				providing an API that allows for the establishment and removal of network
				reserviations.

				Command line flags:
					-C config	-- config file that provides openstack credentials and maybe more
					-c chkpt	-- checkpoint file, last set of reservations
					-f host:port -- floodlight (SDNC) host:port
					-p port		-- tegu listen port (4444)
					-s cookie	-- super cookie
					-v			-- verbose mode

	Date:		20 November 2013
	Author:		E. Scott Daniels

	Mods:		20 Jan 2014 : added support to allow a single VM in a reservation (VMname,any)
							+nnn time now supported on reservation request.
				10 Mar 2014 : converted to per-path queue setting (ingress/egress/middle queues)
				13 Mar 2014 : Corrected 'bug' with setting pledges where both hosts connect to the
							same switch. (bug was that it wasn't yet implemented.)
				03 Apr 2014 : Added endpoint support for reservations and flowmods

	Trivia:		http://en.wikipedia.org/wiki/Tupinambis
*/

package main

import (
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/ipc"
	"github.com/att/tegu/managers"
	"github.com/att/tegu/gizmos"
)

var (
	sheep *bleater.Bleater
)


func usage( version string ) {
	fmt.Fprintf( os.Stdout, "tegu %s\n", version )
	fmt.Fprintf( os.Stdout, "usage: tegu [-C config-file] [-c ckpt-file] [-f floodlight-host] [-p api-port] [-s super-cookie] [-v]\n" )
}

func main() {
	var (
		version		string = "v2.1/14034"
		cfg_file	*string  = nil
		api_port	*string			// command line option vars must be pointers
		verbose 	*bool
		needs_help 	*bool
		fl_host		*string
		super_cookie *string
		chkpt_file	*string

		// various comm channels for threads
		nw_ch	chan *ipc.Chmsg		// network graph manager
		rmgr_ch	chan *ipc.Chmsg		// reservation manager
		osif_ch chan *ipc.Chmsg		// openstack interface
		fq_ch chan *ipc.Chmsg			// flow queue manager

		wgroup	sync.WaitGroup
	)

	sheep = bleater.Mk_bleater( 1, os.Stderr )
	sheep.Set_prefix( "tegu-main" )
	sheep.Add_child( gizmos.Get_sheep( ) )			// since we don't directly initialise the gizmo environment we ask for its sheep

	needs_help = flag.Bool( "?", false, "show usage" )

	chkpt_file = flag.String( "c", "", "check-point-file" )
	cfg_file = flag.String( "C", "", "configuration-file" )
	fl_host = flag.String( "f", "", "floodlight_host:port" )
	api_port = flag.String( "p", "29444", "api_port" )
	super_cookie = flag.String( "s", "", "admin-cookie" )
	verbose = flag.Bool( "v", false, "verbose" )

	flag.Parse()									// actually parse the commandline

	if *needs_help {
		usage( version )
		os.Exit( 0 )
	}

	if( *verbose ) {
		sheep.Set_level( 1 )
	}
	sheep.Baa( 1, "tegu %s started", version )
	sheep.Baa( 1, "http api is listening on: %s", *api_port )

	if *super_cookie == "" {							// must have something and if not supplied this is probably not guessable without the code
		x := "20030217"	
		super_cookie = &x
	}

	nw_ch = make( chan *ipc.Chmsg )					// create the channels that the threads will listen to
	fq_ch = make( chan *ipc.Chmsg, 128 )			// reqmgr will spew requests expecting a response (asynch) only if there is an error, so channel must be buffered
	rmgr_ch = make( chan *ipc.Chmsg, 256 );			// buffered to allow fq to send errors; should be more than fq buffer size to prevent deadlock
	osif_ch = make( chan *ipc.Chmsg )

	err := managers.Initialise( cfg_file, nw_ch, rmgr_ch, osif_ch, fq_ch )		// specific things that must be initialised with data from main so init() doesn't work
	if err != nil {
		sheep.Baa( 0, "ERR: unable to initialise: %s\n", err );
		os.Exit( 1 )
	}

	go managers.Res_manager( rmgr_ch, super_cookie ); 						// manage the reservation inventory
	go managers.Network_mgr( nw_ch, fl_host )								// manage the network graph

	if *chkpt_file != "" {
		my_chan := make( chan *ipc.Chmsg )
		req := ipc.Mk_chmsg( )
	
		req.Send_req( nw_ch, my_chan, managers.REQ_NOOP, nil, nil )		// 'ping' network manager; it will respond after initial build
		req = <- my_chan												// block until we have a response back

		req.Send_req( rmgr_ch, my_chan, managers.REQ_LOAD, chkpt_file, nil )
		req = <- my_chan												// block until the file is loaded

		if req.State != nil {
			sheep.Baa( 0, "ERR: unable to load checkpoint file: %s: %s\n", *chkpt_file, req.State )
			os.Exit( 1 )
		}
	}

	go managers.Fq_mgr( fq_ch, fl_host );
	go managers.Osif_mgr( osif_ch )										// openstack interface
	go managers.Http_api( api_port, nw_ch, rmgr_ch )				// finally, turn on the HTTP interface after _everything_ else is running.

	wgroup.Add( 1 )					// forces us to block forever since no goroutine gets the group to dec when finished (they dont!)
	wgroup.Wait( )
	os.Exit( 0 )
}

