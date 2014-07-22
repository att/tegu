/*
	Mnemonic:	globals.go
	Abstract:	Global things shared by all managers.  Use caution when modifying or adding to iota lists order
				might be important and note where new cosntant blocks are started as the reason is likely
				that iota needs to be reset!

				There is also one initialisation function that is managed here. We cannot make use of the 
				automatic package initialisation mechanism because the inititalisation requires specific
				information from the main which is passed into the init function.  

	Date:		02 December 2013
	Author:		E. Scott Daniels

	Mods:
				07 Jul 2014 - Added support for reservation refresh.
				21 Jul 2014 - Added support list ul caps.
*/

package managers

import (
	"fmt"
	"os"
	
	"forge.research.att.com/gopkgs/clike"
	"forge.research.att.com/gopkgs/ipc"
	"forge.research.att.com/gopkgs/config"
	"forge.research.att.com/gopkgs/bleater"

	"forge.research.att.com/tegu/gizmos"
)

const (
	// message types (requests) that are placed into channel messages. Primary reciver of each type is
	// indicated in parens (except for the more generic types).
	REQ_NOOP		int = -1	// no operation
	_				int = iota	// skip 0
	REQ_RESERVE					// reservation request
	REQ_NETGRAPH				// return the network graph as a jThing (json)
	REQ_HASCAP					// check for reservation capacity
	REQ_ADD						// generic requests may mean slightly different things based on the go-routine receiving the request
	REQ_DEL			
	REQ_GET			
	REQ_CHKPT					// take a checkpoint (res_mgr)
	REQ_LOAD					// load checkpoint file data (res_mgr)
	REQ_NETUPDATE				// new network graph is attached (network)
	REQ_LISTCONNS				// user request a port list for named host	(network)
	REQ_GENMAPS					// generate VM maps (osif) and send out
	REQ_GETIP					// look up the VM name or ID and return the IP address
	REQ_GWMAP					// map that translates mac to ip just for gateway nodes (not included in the vm list)
	REQ_PUSH					// generic push depending on receiver
	REQ_LIST					// generic list depending on receiver
	REQ_GETLMAX					// get max link allocation across the network
	REQ_SETQUEUES				// fqmgr - tickle to cause queues to be set if needed
	REQ_CHOSTLIST				// osif - get a list of compute hosts
	REQ_LISTHOSTS				// network - build a host list that includes vm name, ip, switch(es) and port(s) for each host
	REQ_GEN_QMAP				// network - generate queue info needed by external process to set queues
	REQ_IE_RESERVE				// fq-manager send ingress/egress reservations to skoogi
	REQ_VM2IP					// xlate map VM name | VM ID to IP map is in the request
	REQ_VMID2IP					// xlate map VM-ID to ip is in request
	REQ_IP2VMID					// xlate map IP address to VM-ID
	REQ_VMID2PHOST				// xlate map VM-ID to physical host name
	REQ_IP2MAC					// xlate map IP address to mac
	REQ_GEN_EPQMAP				// generate queue map for end points only (no intermediate queues are generated)
	REQ_SENDALL					// send message to all
	REQ_SENDSHORT				// send a long running request to a single agent (uses only one agent to handle all long running requests
	REQ_SENDLONG				// send a short running request to a single agent (will round robin between all attached agents)
	REQ_IP2MACMAP				// generate an ip to mac translation table and return to requestor
	REQ_MAC2PHOST				// request contains mac to physical host data
	REQ_INTERMEDQ				// setup queues and flowmods on intermediate switches
	REQ_IP2FIP					// request contains a translation of tenant/ip to floating ip
	REQ_FIP2IP					// request contains a translation of floating ip to tenant/ip
	REQ_STATE					// generate some kind of state data back to message sender
	REQ_PAUSE					// put things into a paused mode
	REQ_RESUME					// take things out of a paused mode and resume normal reservation operation.
	REQ_VALIDATE_HOST			// validaate a [token/][project/]hostname string
	REQ_GENCREDS				// generate crdentials
	REQ_VALIDATE_ADMIN			// validate an admin token
	REQ_PNAME2ID				// translate project (user, tenant, etc.) to ID
	REQ_SETULCAP				// set a user link capacity
	REQ_XLATE_HOST				// translate a [token/][project/]hostname into ID/hostname without validation of token if it exits.
	REQ_PLEDGE_LIST				// causes res mgr to generate a list of pledges based on a host name
	REQ_YANK_RES				// yank out a reservation causing flow-mods to drop
	REQ_LISTULCAP				// user link capacity list
	
)

const (
	ONE_GIG		int64 = 1024 * 1024 * 1024

	version 	string = "v3.0/17224"
)


// fq_mgr constants	(resets iota)
const (
				// offsets into the array of data passed to fq_mgr on requests
	FQ_IP1		int = iota		// ip address of host 1					(ie proactive reservation request)
	FQ_IP2						// ip address of host 2
	FQ_EXPIRY					// reservation expiry time 
	FQ_SPQ						// queue to map traffic to
	FQ_ID						// id used if reporting error asynch
	FQ_DIR_IN					// bool flag that indicates whether the flowmod direction is into switch or out of switch
	FQ_DSCP						// user supplied dscp that the data should have on egress
	FQ_EXTIP					// an external IP that is needed to setup flow mods when session traveling through a gateway
	FQ_EXTTY					// external IP type used in fmod command (either -D or -S)
	FQ_TPSPORT					// transport source port number
	FQ_TPDPORT					// transport dest port number

	FQ_SIZE						// CAUTION:  this must be LAST as it indicates the size of the array needed

	FQ_QLIST	int = 0			// the list of current queue settings 	(set queues)
)

var (
	shell_cmd	string = "/bin/ksh"						// preferred shell, cfg can override in default section
	empty_str	string = ""								// go prevents &"", so these make generating a pointer to default strings easier
	default_sdn	string = "localhost:8080"				// default controller (skoogi)
	local_host	string = "localhost"

	cfg_data	map[string]map[string]*string			// things read from the configuration file

	/* 
		Channels that various goroutines listen to. Global so that all goroutines have access to them.
	*/
	nw_ch		chan	*ipc.Chmsg		// network 
	rmgr_ch		chan	*ipc.Chmsg		// reservation manager 
	osif_ch		chan	*ipc.Chmsg		// openstack interface
	fq_ch		chan	*ipc.Chmsg		// flow and queue manager
	am_ch		chan	*ipc.Chmsg		// agent manager channel

	tklr	*ipc.Tickler				// tickler that will drive periodic things like checkpointing

	pid int = 0							// process id for use in generating reservation names uniqueue across invocations
	res_nmseed	int = 0					// reservation name sequential value
	res_paused	bool = false			// set to true if reservations are paused

	super_cookie	*string; 			// the 'admin cookie' that the super user can use to manipulate a reservation

	tegu_sheep	*bleater.Bleater		// parent sheep that controls the 'master' bleating volume and is used by 'library' functions (allocated in init below)
	net_sheep	*bleater.Bleater		// indivual sheep for each goroutine (each is responsible for allocating their own sheep)
	am_sheep	*bleater.Bleater
	fq_sheep	*bleater.Bleater
	osif_sheep	*bleater.Bleater
	rm_sheep	*bleater.Bleater
	http_sheep	*bleater.Bleater
	qm_sheep	*bleater.Bleater

	/*
		http manager needs globals because the http callback doesn't allow private data to be passed
	*/
	priv_auth *string;					// type of authorisation needed for privledged commands (pause, resume, etc.)
)

//--------------------------------------------------------------------------------------------------------------------------

/*
	Sets up the global variables needed by the whole package. This should be invoked by the 
	main tegu function (main/tegu.go).

	CAUTION:  this is not implemented as an init() function as we must pass information from the 
			main to here.  
*/
func Initialise( cfg_fname *string, nwch chan *ipc.Chmsg, rmch chan *ipc.Chmsg, osifch chan *ipc.Chmsg, fqch chan *ipc.Chmsg, amch chan *ipc.Chmsg ) (err error)  {

	err = nil

	def_log_dir := "."
	log_dir := &empty_str

	nw_ch = nwch;		
	rmgr_ch = rmch
	osif_ch = osifch
	fq_ch = fqch
	am_ch = amch
	

	tegu_sheep = bleater.Mk_bleater( 1, os.Stderr )		// the main (parent) bleater used by libraries and as master 'volume' control
	tegu_sheep.Set_prefix( "tegu" )

	pid = os.Getpid()							// used to keep reservation names unique across invocations

	tklr = ipc.Mk_tickler( 30 )				// shouldn't need more than 30 different tickle spots
	tklr.Add_spot( 2, rmgr_ch, REQ_NOOP, nil, 1 )	// a quick burst tickle to prevent a long block if the first goroutine to schedule a tickle schedules a long wait

	if cfg_fname != nil {
		cfg_data, err = config.Parse2strs( nil, *cfg_fname )		// capture config data as strings -- referenced as cfg_data["sect"]["key"] 
		if err != nil {
			err = fmt.Errorf( "unable to parse config file %s: %s", *cfg_fname, err )
			return
		}

		if p := cfg_data["default"]["shell"]; p != nil {
			shell_cmd = *p
		}
		if p := cfg_data["default"]["verbose"]; p != nil {
			 tegu_sheep.Set_level( uint( clike.Atoi( *p ) ) )
		}
		if log_dir = cfg_data["default"]["log_dir"]; log_dir == nil {
			log_dir = &def_log_dir
		}
	} else {
		cfg_data = nil
	}

	tegu_sheep.Add_child( gizmos.Get_sheep( ) )						// since we don't directly initialise the gizmo environment we ask for its sheep
	if *log_dir  != "stderr" {										// if overriden in config
		lfn := tegu_sheep.Mk_logfile_nm( log_dir, 86400 )
		tegu_sheep.Baa( 1, "switching to log file: %s", *lfn )
		tegu_sheep.Append_target( *lfn, false )						// switch bleaters to the log file rather than stderr
		go tegu_sheep.Sheep_herder( log_dir, 86400 )				// start the function that will roll the log now and again
	}

	return
}
