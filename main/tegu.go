// vi: sw=4 ts=4:

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

	Mods:		20 Jan 2014 : Added support to allow a single VM in a reservation (VMname,any)
							+nnn time now supported on reservation request.
				10 Mar 2014 : Converted to per-path queue setting (ingress/egress/middle queues).
				13 Mar 2014 : Corrected 'bug' with setting pledges where both hosts connect to the
							same switch. (bug was that it wasn't yet implemented.)
				03 Apr 2014 : Added endpoint support for reservations and flowmods.
				05 May 2014 : Changes to support merging gateways into the graph and for receiving
							responses back from the agent.
				13 May 2014 : Changes to support dscp-exit value supplied on reservation.
				16 May 2014 : Corrected bug with specifying the "exit" dscp value.
				18 May 2014 : Now supports cross tenant reservations.
				29 May 2014 : Now supports default openstack value propigation in config file.
				05 Jun 2014 : Added pause/resume ability.
				06 Jun 2014 : Added TLS/SSL support.
				09 Jun 2014 : Added token authorisation support for reservations.
				11 Jun 2014 : All paths option added-- reservation will be based on capacity over all
							paths between h1 and h2.
				16 Jun 2014 : Added abilityt to authorise privledged commands with a token generated using
							the default (admin) user name given in the config file.
				17 Jun 2014 : Added support for transport ports on reservation host names.
				25 Jun 2014 : Added user (project, tenant, whatever) level caps on each link (gak).
				07 Jul 2014 : Added refresh API request.
				15 Jul 2014 : Added support for parital reservation path when only one endpoint is given a
							valid token on the reservation request.
				21 Jul 2014 : Fixed bug -- checkpoint not including user link caps
				29 Jul 2014 : Added mlag support.
				13 Aug 2014 : Changes to include network hosts in the list of hosts (incorporate library
							changes)
				15 Aug 2014 : Bug fix (201) and stack dump fix (nil ptr in osif).
				19 Aug 2014 : Bug fix (208) prevent duplicate tegu's from running on same host/port.
				20 Aug 2014 : Bug fix (210) shift dscp values properly in the match part of a flowmod.
				22 Aug 2014 : Added protocol support to steering. (steer)
				27 Aug 2014 : Fq-mgr changes to allow for metadata checking on flowmods.
				30 Aug 2014 : Pick up bug fix made to ostack library.
				03 Sep 2014 : Corrected bug in resmgr/fqmgr introduced with 27 Aug changes (transport type ignored).
				05 Sep 2014 : Tweak to link add late binding port to pick up the lbp when port < 0 rather than 0.
				08 Sep 2014 : Corrected bugs with tcp oriented proto steering (dest baesd). (steer)
				23 Sep 2014 : Support for rate limiting bridge
				29 Sep 2014 : Nil pointer exception (bug #216) corrected (gizmo change)
				30 Sep 2014 : Deal with odd hostnames that were being returned by ccp's version of openstack.
				09 Oct 2014 : Bug fix (228) -- don't checkpoint until all initialised.
				14 Oct 2014 : Rebuild to pick up library changes that get network 'hosts'
							as host only where OVS is running and not all network hosts.
				19 Oct 2014 : Added bidirectional bandwith support (bug 228). (version bump to 3.0.1 because of
							extra testing needed.)
				23 Oct 2014 : Added better diagnostics to the user regarding capacity rejection of reservation (bug 239)
				29 Oct 2014 : Corrected issue where vlan id was being set when both VMs are on the same switch (bug 242)
				30 Oct 2014 : Corrected bug with setting the source/dest flag for external IP addresses in flowmod req (bug 243)
				03 Nov 2014 : Merged steering and lite branches into steering branch; version bump to 3.2.
				04 Nov 2014 : Build to pick up ostack library change.
				10 Nov 2014 : Build to pick up ostack library change (small tokens).
				11 Nov 2014 : Change to support host name suffix in fqmgr.
				12 Nov 2014 : Change to strip phys host suffix from phys map.
				13 Nov 2014 : Correct out of bounds excpetion in fq-manager.
				17 Nov 2014 : Converted the openstack interface to a lazy update method rather than attempting to
							prefetch all of the various translation maps and then to keep them up to date.
				19 Nov 2014 : Correct bug in checkpoint path attachment to reservation.
				24 Nov 2014 : Floating IP address requirement for cross tenant reservations, and reservations between VM and an
							 external host has been modified.
				04 Dec 2014 : Uses enabled host list from ostack inteface.
				05 Dec 2014 : Added work round for keystone/privs token investigation issue in AIC once they started using LDAP.
				07 Dec 2014 : Corrected default tickler time for set intermediate queues.
				16 Dec 2014 : Corrected bugs in lazy update translation of VM ID to ip.
				08 Jan 2015 : Corrected a bug that was causing the wrong gateway to be slected as the endpoint for an
							external reservation.
				09 Jan 2015 : Augmented the 1/8 fix to not assume the subnet list is limited by project in the creds.
							Changes to allow for finding all IP addresses assigned to a VM, not just the first.
				16 Jan 2015 : Added support for mask on the transport port specification.
							Changed meta table flow-mod generation to happen with queue changes and to push only to nodes
							in the queue list rather than to all nodes.
				26 Jan 2015 : Corrected bug in fq_mgr that was causing inbound data to use meta marking in base+1 rather than base table.
				27 Jan 2015 : Allow bandwidth to be specified as a decimal (e.g 10.2M) on a reservation command.
				29 Jan 2015 : Changes to send fmod requests to the ssh-broker enabled agent.
				01 Feb 2015 : Corrected bug introduced when host name removed from fmod command (agents with ssh-broker change).
				09 Feb 2015 : Added work round to deal with OVS/openflow small hard timeout limit.
				10 Feb 2015 : Corrected bug with refresh.
				18 Feb 2015 : Corrected bug with slice index and enabled both host name types on create queue list.
				24 Feb 2015 : Added mirroring (version => 3.1.3).
				11 Mar 2015 : Corrected bug in network manager causing coredump when gateway info missing on bleat message.
				25 Mar 2015 : Added support for IPv6 in bandwidth reservations, and new bandwidth flow-mod script in the agent.
				31 Mar 2015 : Added abilty to force a load of any specific project's VM data into the graph, or all that are
							known to Tegu. 
				03 Apr 2015 : Added role verification for graph and listhosts to allow any admin to submit these from afar.
				10 Apr 2015 : Added ability to delete reservation via post, corrected slice bounds error (tokeniser library fix).

	Version number "logic":
				3.0		- QoS-Lite version of Tegu
				3.0.1	- QoS-Lite version of Tegu with lazy openstack information gathering (17 Nov 2014)
				3.1		- QoS-Lite with steering added
				3.1.3	- QoS-Lite with steering and mirroring API added
				3.2		- QoS-Lite with steering and WACC support added
	Trivia:		http://en.wikipedia.org/wiki/Tupinambis
*/

package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"codecloud.web.att.com/gopkgs/bleater"
	"codecloud.web.att.com/gopkgs/ipc"
	"codecloud.web.att.com/tegu/managers"
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
		version		string = "v3.1.3/14105"		// 3.1.x == steering branch version (.2 steering only, .3 steering+mirror)
		cfg_file	*string  = nil
		api_port	*string						// command line option vars must be pointers
		verbose 	*bool
		needs_help 	*bool
		fl_host		*string
		super_cookie *string
		chkpt_file	*string

		// various comm channels for threads -- we declare them here so they can be passed to managers that need them
		nw_ch	chan *ipc.Chmsg		// network graph manager
		rmgr_ch	chan *ipc.Chmsg		// reservation manager
		osif_ch chan *ipc.Chmsg		// openstack interface
		fq_ch chan *ipc.Chmsg		// flow queue manager
		am_ch chan *ipc.Chmsg		// agent manager channel

		wgroup	sync.WaitGroup
	)

	sheep = bleater.Mk_bleater( 1, os.Stderr )
	sheep.Set_prefix( "main/3.0" )

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

	nw_ch = make( chan *ipc.Chmsg, 128 )					// create the channels that the threads will listen to
	fq_ch = make( chan *ipc.Chmsg, 1024 )			// reqmgr will spew requests expecting a response (asynch) only if there is an error, so channel must be buffered
	am_ch = make( chan *ipc.Chmsg, 1024 )			// agent manager channel
	rmgr_ch = make( chan *ipc.Chmsg, 1024 );			// buffered to allow fq to send errors; should be more than fq buffer size to prevent deadlock
	osif_ch = make( chan *ipc.Chmsg, 1024 )

	err := managers.Initialise( cfg_file, &version, nw_ch, rmgr_ch, osif_ch, fq_ch, am_ch )		// specific things that must be initialised with data from main so init() doesn't work
	if err != nil {
		sheep.Baa( 0, "ERR: unable to initialise: %s\n", err );
		os.Exit( 1 )
	}

	go managers.Http_api( api_port, nw_ch, rmgr_ch )				// start early so we bind to port quickly, but don't allow requests until late
	go managers.Res_manager( rmgr_ch, super_cookie ); 				// manage the reservation inventory
	go managers.Osif_mgr( osif_ch )									// openstack interface; early so we get a list of stuff before we start network
	go managers.Network_mgr( nw_ch, fl_host )						// manage the network graph
	go managers.Agent_mgr( am_ch )
	go managers.Fq_mgr( fq_ch, fl_host );

	my_chan := make( chan *ipc.Chmsg )								// channel and request block to ping net, and then to send all sys up
	req := ipc.Mk_chmsg( )

	/*
		Block until the network is initialised. We need to do this so that when the checkpoint file is read reservations
		can be added without missing network pieces.  Even if there is no checkpoint file, or it's empty, blocking
		prevents reservation rejections because the network graph isn't in working order.  At the moment, with lazy
		udpating, the block is until we have a physical host map back from the agent world.  This can sometimes take
		a minute or two.
	*/
	for {																	// hard block to wait on network readyness
		req.Response_data = 0
		req.Send_req( nw_ch, my_chan, managers.REQ_STATE, nil, nil )		// 'ping' network manager; it will respond after initial build
		req = <- my_chan													// block until we have a response back

		if req.Response_data.(int) == 2 {									// wait until we have everything that the network needs to build a reservation
			break
		}

		sheep.Baa( 2, "waiting for network to initialise: need state 2, current state = %d", req.Response_data.(int)  )
		time.Sleep( 5 * time.Second )
	}

	if *chkpt_file != "" {
		sheep.Baa( 1, "network initialised, sending chkpt load request" )
		req.Send_req( rmgr_ch, my_chan, managers.REQ_LOAD, chkpt_file, nil )
		req = <- my_chan												// block until the file is loaded

		if req.State != nil {
			sheep.Baa( 0, "ERR: unable to load checkpoint file: %s: %s\n", *chkpt_file, req.State )
			os.Exit( 1 )
		}
	} else {
		sheep.Baa( 1, "network initialised, opening up system for all requests" )
	}

	req.Send_req( rmgr_ch, nil, managers.REQ_ALLUP, nil, nil )		// send all clear to the managers that need to know
	managers.Set_accept_state( true )								// http doesn't have a control loop like others, so needs this

	wgroup.Add( 1 )					// forces us to block forever since no goroutine gets the group to dec when finished (they dont!)
	wgroup.Wait( )
	os.Exit( 0 )
}
