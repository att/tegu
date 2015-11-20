// vi: sw=4 ts=4:
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

	Mnemonic:	datacache.go
	Abstract:	Provides an interface to a cassendra database. Mk_dcache will return 
				a pointer to the single instance of the dcache struct and will
				create it if needed. The parms can be nil, but allow the tegu main 
				to pass in the config file struct and sheep to allow for configuration
				at runtime.

				WARNING:
					experiments with using json to insert things into cassandra have provend to 
					be frustrating at best. Don't use json for insert/update.

	Date:		19 November 2015
	Author:		E. Scott Daniels

	Mods:
*/

package datacache

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/clike"
	"github.com/att/gopkgs/token"

	//"github.com/att/tegu/gizmos"

	"github.com/gocql/gocql"			// cassandra db interface (requires go 1.4 or later)
)

/*
	Our needed configuration information. Some from the tegu config and other
	stuff gathered along the way.
*/
type Dcache struct {
	sheep	*bleater.Bleater
								// these things come from tegu config file
	db_hosts	string			// where the various bits of cassandra are running
	port	string				// port the db is listening on
	tcn		string				// tegu cluster name (our namespace in the db)
	rep_factor int				// replication factor

									// these added during db connect etc.
	cluster	*gocql.ClusterConfig	// cassendra cluster information
	sess	*gocql.Session
	connected	bool				// set to true once connected
	mu		*sync.Mutex
}

var (
	global_instance	*Dcache = nil	// we only ever create one instance
)


/*
	Create the base struct sussing out things from the tegu config if the data is passed to us.
*/
func Mk_dcache( cfg_data map[string]map[string]*string, master_sheep *bleater.Bleater ) ( dc *Dcache ) {

	if global_instance != nil {
		return global_instance
	}

	dc = &Dcache {
		sheep:		master_sheep,
		db_hosts:	"localhost",
		port:		"9042",
		rep_factor:	1,
		tcn:		"tegu",
		mu:			&sync.Mutex{},
	}

	dc.sheep = bleater.Mk_bleater( 0, os.Stderr )		// allocate our bleater and attach it to the master
	dc.sheep.Set_prefix( "datacache" )
	if master_sheep != nil {
		master_sheep.Add_child( dc.sheep )				// we become a child so that if the master vol is adjusted we'll react too
	}

	if cfg_data != nil {
		if cfg_data["datacache"] != nil {								// things we pull from the default section
			if p := cfg_data["datacache"]["tcn"]; p != nil {
				dc.tcn = *p
			}
			if p := cfg_data["datacache"]["port"]; p != nil {
				dc.port = *p
			}
			if p := cfg_data["datacache"]["hosts"]; p != nil {
				dc.db_hosts = *p
			}
			if p := cfg_data["datacache"]["rep_factor"]; p != nil {
				dc.rep_factor = clike.Atoi( *p )
			}
		}
	}

	global_instance = dc
	return
}


// ---------------------- private  ------------------------------------------------------------------------------


/*
	Ensure that the tcn (tegu cache name) exists inside of cassandra.
*/
func ( dc *Dcache ) set_keyspace( ) ( err error ) {
	if dc == nil {
		return fmt.Errorf( "no data cache struct" )
	}

	dc.cluster.Keyspace = "system"								// switch to system land for this call
	dc.cluster.Timeout = 20 * time.Second
    sess, err := dc.cluster.CreateSession()						// and create a new session
	if err != nil {
		dc.sheep.Baa( 1, "CRI: unable to create a session to setup datacache key space: %s", err )
		return
	}

	q := fmt.Sprintf( `CREATE KEYSPACE %s WITH replication = { 'class' : 'SimpleStrategy', 'replication_factor' : %d }`, dc.tcn, dc.rep_factor ) 
	err = sess.Query( q ).Exec()

	dc.cluster.Keyspace = dc.tcn								// back to our space now

	return nil
}

/*
	Ensure that our required tables are in the cache.  There has to be a better way than this, 
	but maybe not. 
*/
func ( dc *Dcache ) ensure_tables() ( err error ) {
	err  = dc.sess.Query( fmt.Sprintf( `CREATE TABLE ulcaps ( project text PRIMARY KEY, pctg int)` ) ).Exec()


	// TODO: add the rest of tegu tables
	return nil
}

func ( dc *Dcache ) connect( ) (state bool, err error) {

	state = false 
	err = nil

	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.connected {						// someone beet us to the punch
		return true, nil
	}

	dc.sheep.Baa( 1, "connecting to: %s", dc.db_hosts )
	nhosts, hosts := token.Tokenise_qsep( dc.db_hosts, ", "  ) 				// split on either commas or spaces
	if nhosts < 1 {
		dc.sheep.Baa( 0, "CRI: no hosts listed for datacache" )
		return false, fmt.Errorf( "no seed hosts in the list" )
	}

    dc.cluster = gocql.NewCluster( hosts[0] )			// this is stilly; internally to cluster is a slice, but the function requires individual strings; wtf?
    if dc.cluster == nil {
		dc.sheep.Baa( 0, "CRI: unable to allocate a datacache cluster configuration" )
		return false, fmt.Errorf( "cluster allocation failed" )
    }

    dc.cluster.Keyspace = dc.tcn							// set our namespace
    dc.cluster.Hosts = hosts								// work round their var args interface
	switch nhosts {
		case 1:
    		dc.cluster.Consistency = gocql.One 				// possible values: Any, One, Two, Three, Quorum, All, LocalQuorum EachQuorum LocalOne

		case 2:
    		dc.cluster.Consistency = gocql.One

		case 3:
    		dc.cluster.Consistency = gocql.Two

		default:
    		dc.cluster.Consistency = gocql.Quorum
	}

    dc.cluster.ProtoVersion = 4
    dc.cluster.Port = clike.Atoi( dc.port )

	err = dc.set_keyspace()									// must ensure that our keyspace exists
	if err != nil {
		dc.sheep.Baa( 0, "CRI: unable to set keyspace: %s", err )
		return false, err
	}

    dc.sess, err = dc.cluster.CreateSession()

    if err != nil {
		dc.sheep.Baa( 0, "CRI: unable to create a session to the datacache: %s", err )
		return false, err
    }

	err = dc.ensure_tables( )
	if err != nil {
		dc.sheep.Baa( 0, "CRI: unable to ensure that our tables exist in the bloody db: %s", err )
		return false, err
	}

	dc.connected = true
	return true, nil
}

// ---------------------- public ------------------------------------------------------------------------------

/*
	Returns the current connectivity state and if not connected kicks our listener to try another
	time to connect.
*/
func ( dc * Dcache ) Is_connected( ) ( bool ) {
	if !dc.connected {
		ok, _ :=  dc.connect( )
		return ok
	}

	return true
}

/*
	Fetch the ulcap for the project from the datacache.
*/
func ( dc *Dcache ) Get_ulcap( project string ) ( pctg int, err error ) {

	pctg = 0

	if dc == nil {
		return 0, fmt.Errorf( "nil ptr to struct passed" )
	}

	if ! dc.connected {
		if ok, err :=  dc.connect(); ! ok {
			return 0, err
		}
	}

	err = dc.sess.Query( `SELECT pctg FROM ulcaps WHERE project = ? LIMIT 1`, project ).Consistency(gocql.One).Scan( &pctg )
    if err != nil {
		dc.sheep.Baa( 2, "unable to find ulcap for project: %s", project )
		err = fmt.Errorf( "ulcap not found for %s: %s", project, err )
    } else {
		dc.sheep.Baa( 1, ">>>> found ulcap in datacache: %s %d", project, pctg )
	}

	return pctg, err
}

/*
	Returns a map of all user limit capacities keyed by project id.
*/
func ( dc *Dcache ) Map_ulcaps( ) ( m map[string]int, err error ) {
	if dc == nil {
		return nil, fmt.Errorf( "datacache struct was nil" )
	}

	var	(
		proj string
		pctg int
	)

	m = make( map[string]int, 64 )			// 64 is a hint not a hard limit

	dc.sheep.Baa( 1, "building map...." )
    iter := dc.sess.Query( `select project, pctg  from ulcaps` ).Consistency(gocql.One).Iter()
    for iter.Scan( &proj, &pctg )  {
		m[proj] = pctg
		dc.sheep.Baa( 2, "dug ulcap from datacache: %s = %d", proj, pctg )
    }

	return m, nil
}

/*
	Put the ulcap for the project into the datacache.
	Data is expected to be a string with project,value.
*/
func ( dc *Dcache ) Set_ulcap( project string, val int ) ( err error ) {

	if dc == nil {
		return fmt.Errorf( "no struct passed to set_ulcap" )
	}

	if !dc.connected {
		if  ok, err := dc.connect(); ! ok {
			return err
		}
	}

	if val > 100 {
		val = 100
	} else {
		if val < 0 {
			val = 0
		}
	}

    err = dc.sess.Query( `INSERT INTO ulcaps (project, pctg) VALUES (?, ?)`, project, val ).Exec()
    if err != nil {
		dc.sheep.Baa( 2, "unable to set ulcap for project: %s", project )
    }

	return  nil
}

