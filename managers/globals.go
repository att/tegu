package managers

import (
	"fmt"
	"os"
	
	"forge.research.att.com/gopkgs/clike"
	"forge.research.att.com/gopkgs/ipc"
	"forge.research.att.com/gopkgs/config"
	"forge.research.att.com/gopkgs/bleater"
)

// global constants and variables -- no such thing as protected or compile unit static it seems, so by 
// putting the globals in a separate module it should be more obvious that these are shared across all
// package members (as though all tegu source was in the same file).

const (
	// message types (requests) that are placed into channel messages. Primary reciver of each type is
	// indicated in parens (except for the more generic types).
	REQ_NOOP		int = -1	// no operation
	REQ_RESERVE		int = 1		// reservation request
	REQ_NETGRAPH	int = 2		// return the network graph as a jThing (json)
	REQ_HASCAP		int = 3		// check for reservation capacity
	REQ_ADD			int = 4		// generic requests may mean slightly different things based on the go-routine receiving the request
	REQ_DEL			int = 5
	REQ_GET			int = 6
	REQ_CHKPT		int = 7		// take a checkpoint (res_mgr)
	REQ_LOAD		int = 8		// load checkpoint file data (res_mgr)
	REQ_NETUPDATE	int = 9		// new network graph is attached (network)
	REQ_LISTCONNS	int = 10	// user request a port list for named host	(network)
	REQ_VM2IP		int = 11	// map VM names and IDs to IP addresses (osif)
	REQ_GETIP		int = 12	// look up the VM name or ID and return the IP address
	REQ_PUSH		int = 13	// generic push depending on receiver
	REQ_LIST		int = 14	// generic list depending on receiver
	REQ_GETLMAX		int = 15	// get max link allocation across the network
	REQ_SETQUEUES	int = 16	// fqmgr - tickle to cause queues to be set if needed
	REQ_CHOSTLIST	int = 17	// osif - get a list of compute hosts
	REQ_HOSTLIST	int = 18	// network - build a host list that includes vm name, ip, switch(es) and port(s) for each host
	REQ_GEN_QMAP	int = 19	// network - generate queue info needed by external process to set queues
	REQ_IE_RESERVE	int	= 20	// fq-manager send ingress/egress reservations to skoogi

	ONE_GIG		int64 = 1024 * 1024 * 1024

	version 	string = "v2.0/13174"
)


// fq_mgr constants
const (
				// offsets into the array of data passed to fq_mgr on requests
	FQ_IP1		int = 0			// ip address of host 1					(ie proactive reservation request)
	FQ_IP2		int = 1			// ip address of host 2
	FQ_EXPIRY	int = 2			// reservation expiry time 
	FQ_SPQ		int = 3			// queue to map traffic to
	FQ_ID		int	= 4			// id used if reporting error asynch

	FQ_QLIST	int = 0			// the list of curren queue settings 	(set queues)
)

var (
	shell_cmd	string = "/bin/ksh"						// preferred shell, cfg can override in default section
	empty_str	string = ""								// go prevents &"", so these make generating a pointer to default strings easier
	default_sdn	string = "localhost:8080"				// default controller (skoogi)
	local_host	string = "localhost"

	cfg_data	map[string]map[string]*string			// things read from the configuration file

	/* 
		channels that various goroutines listen to. 
	*/
	nw_ch		chan	*ipc.Chmsg		// network 
	rmgr_ch		chan	*ipc.Chmsg		// reservation manager 
	osif_ch		chan	*ipc.Chmsg		// openstack interface
	fq_ch		chan	*ipc.Chmsg		// flow and queue manager

	tklr	*ipc.Tickler					// tickler that will drive periodic things like checkpointing

	pid int = 0							// process id for use in generating reservation names uniqueue across invocations
	res_nmseed	int = 0					// reservation name sequential value

	super_cookie	*string; 				// the 'admin cookie' that the super user can use to manipulate a reservation

	tegu_sheep	*bleater.Bleater			// parent sheep that controls the 'master' bleating volume and is used by 'library' functions
	net_sheep	*bleater.Bleater			// indivual sheep for each goroutine
	fq_sheep	*bleater.Bleater
	osif_sheep	*bleater.Bleater
	rm_sheep	*bleater.Bleater
	http_sheep	*bleater.Bleater
)

/*
	Sets up the global variables needed by the whole package. This should be invoked by the 
	main tegu function (main/tegu.go).

	CAUTION:  this is not implemented as an init() function as we must pass information from the 
			main to here.  
*/
func Initialise( cfg_fname *string, nwch chan *ipc.Chmsg, rmch chan *ipc.Chmsg, osifch chan *ipc.Chmsg, fqch chan *ipc.Chmsg ) (err error)  {

	err = nil

	nw_ch = nwch;		
	rmgr_ch = rmch
	osif_ch = osifch
	fq_ch = fqch

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
	} else {
		cfg_data = nil
	}

	return
}
