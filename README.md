
Tegu
====

Tegu is a reservation manager which provides the ability to create and manage:
 * quality of service bandwidth reservations between network endpoints
 * flow steering reservations
 * wide area network stitching
 * port mirroring reservations

Tegu uses an underlying agent, also included in this repository, to directly manage the 
physical components (Open vSwitch or physical switches) as is needed to implement the
reservations.
The underlying agent scripts are contained in the agent directory and the tegu_agent
binary is in the main directory. 

Directory Overview
------------------

The Tegu source is divided into the following subdirectories/packages:  

#### agent/bandwidth  
This directory contains various direct interfaces (shell scripts) to things like OVS,
Arista switches, Floodlight (skoogie), etc.

#### agent/mirror  
This directory contains shell scripts used to create and remove mirrors to ports in an
OpenStack environment.

#### doc  
The manual pages for the executables *rjprt*, *tegu*, *tegu_req* and a manual page
describing the Tegu API.

#### gizmos  
Source files which implement objects, interfaces and the functions that operate directly
on them (link, host, switch, pledge, etc.).

#### main  
Entry point functions (*tegu*, *tegu_agent*, and *rjprt*).
	
#### managers  
Functions that are driven as goroutines and thus implement major components of the
application (reservation manager, fq manager, etc.).

#### support  
Regressions tests.

#### system  
Scripts used to start, stop, and manage Tegu in a Linux environment, as well as the
*tegu_ha* Python script.

File Overview
-------------

Here is an overview of the purpose of some of the .go files in this repository.

#### gizmos directory  

__fence.go__ - Implements a user limit fence mechanism.  
__flight_if.go__ - A floodlight interface providing methods that allow queries to
the controller for gathering link and host information.  
__host.go__ - Represents a single host in the network graph and in a path.  
__init.go__ - Package level initialization.  
__link.go__ - Represents a link between switches in the network graph and in a path.
__lite.go__ - Functions that were implemented quickly to support tegu-lite.
These probably should be moved to separate files, or likely into tools, but during the
hasty implementation of -lite it was easier to keep them bunched here.  
__mbox.go__ - Middlebox representation for steering reservations.
__obligation.go__ - Used to manage an obligation of something over time;
references many time slices.  
__path.go__ - Manages a path that has been created with a given amount of bandwith.
__pledge.go__ - An interface representing a reservation tracked by resmgr.
Implemented by the various pledge types in the pledge_* files.  
__pledge_window.go__ - Manages a time window for pledges and provides basic
*is_active*, *is_expired* functions.  
__queue.go__ - Manages information needed to set individual queues for a reservation.
__spq.go__ - A very simple object which allows the return of queue information to
a caller in a single bundle (presently, just the struct, no functions exist).  
switch.go__ - Represents a switch in the network graph.  
__time_slice.go__ - A single range of time for which a given amount of bandwith
has been allocated.  
__tools.go__ - Some generic tools but not generic enough to put in *gopkgs*.

#### managers directory  

__fq_mgr.go__ - Flowmod/queue manager.  
__fq_mgr_steer.go__ - Steering based FQ-mgr support.  
__fq_req.go__ - Fqmgr request structure and related functions.  
__globals.go__ - Constants and a few globals shared by \*.go in this directory.  
This module also contains the initialisation function that sets all globals up.  
__http_api.go__ - Provides the HTTP server, and code to serve URL's under */tegu/api*.  
__http_mirror_api.go__ -  The HTTP interface for mirroring.  
__network.go__ - Manages the network graph.  
__net_req.go__ - Network manager request struct and related functions.  
__res_mgr.go__ - Provides the reservation management logic, supplemented by	three support modules:
*res_mgr_bw.go*, *res_mgr_mirror.go*, and *res_mgr_steer.go*.  
__osif.go__ - OpenStack interface manager.  
__osif_proj.go__ - Project specific OpenStack interface functions.  


Building Tegu
-------------

The Tegu source depends on a set of Go packages that were developed along with Tegu, 
but are general enough to warrant their not being included here.
They are all a part of the `github.com/att/gopkgs` package library.
To use them, clone the git project as described below. 
They will be referenced as needed during the build process (unlike C, there is no need
to build a library to link against).
You should be able to do `go get github.com/att/gopkgs` to pull them down.

### Go Environment  
The GOPATH variable must be set to the top level directory in your source tree.
Within that directory there should be src, bin, and pkg directories. 
Under src there should be a github.com directory which will hold all of your
Go related repositories that are checked out of github.

For example:  

	export GOPATH=$HOME/godev
	cd $GOPATH
	mkdir github.com
	cd github.com

	# fork a copy of the tegu and gopkgs first!!!

	# replace XXXXX with your user id, then clone your forks 
	git clone https://XXXXXX@github.com/~XXXXXX/tegu.git
	git clone https://XXXXXX@github.com/~XXXXXX/gopkgs.git

	cd tegu
	git checkout master

Build Tegu by:

	go build main/rjprt.go   		# builds the rjprt binary
	go build main/tegu.go   		# builds the tegu binary
	go build main/tegu_agent.go		# builds the tegu agent binary

What is a Tegu?
---------------

A type of lizard (https://en.wikipedia.org/wiki/Tegu).
