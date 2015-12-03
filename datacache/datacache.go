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


				The functions in this module provide the base interface to the datacache. Other
				modules in the package prodvide the funcitons for specific data types (e.g.
				endpoints and reservations).

	Date:		19 November 2015
	Author:		E. Scott Daniels

	Mods:
*/

package datacache

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/att/gopkgs/bleater"
	"github.com/att/gopkgs/clike"
	"github.com/att/gopkgs/token"
	"github.com/att/gopkgs/transform"

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


// ---------------------- utility  ------------------------------------------------------------------------------
/*
	Takes a map and generates a string that is properly quoted for a cassandra map table entry.
	This function does _NOT_ add the leading trailing braces as the caller may need to build several
	strings for one submission.  

	These don't seem to be needed as the go implementation accepts a map directly and does this
	conversion.
*/
func smap2string( m map[string]string ) (string) {
	s := ""
	sep := ""
	for k, v := range m {
		s += fmt.Sprintf( `%s '%s':'%s'`, sep, k, v )
		sep = ","
	}

	return s
}

/*
	Takes a map of string (key) to string pointers and gernerates a cassandra map table entry.
	This function does _NOT_ add the leading trailing braces as the caller may need to build several
	strings for one submission.  
*/
func spmap2string( m map[string]*string ) (string) {
	s := ""
	sep := ""
	for k, v := range m {
		s += fmt.Sprintf( `%s '%s':'%s'`, sep, k, *v )
		sep = ","
	}

	return s
}

/*
	Takes a map of string (key) to interface and builds a cassandra map table entry.
	This function does _NOT_ add the leading trailing braces as the caller may need to build several
	strings for one submission.  
*/
func imap2string( imap map[string]interface{} ) ( string ) {
	s := ""
	sep := ""
	vs := ""
	for k, v := range imap {
		switch sv := v.(type) {					// convert to specific value type and process
			case int, int64:
				vs = fmt.Sprintf( "%d", sv )

			case float64:
				vs = fmt.Sprintf( "%f", sv )

			case string:
				vs = sv

			case *string:
				 vs = *sv

			case bool:
				vs = fmt.Sprintf( "%v", sv )

			case map[string]string:
				vs = smap2string( sv )

			case map[string]*string:
				vs = spmap2string( sv )
		}

		s += fmt.Sprintf( `%s '%s':'%s'`, sep, k, vs )
		sep = ","
	}

	return s
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

	if err != nil {
		if strings.Index(  err.Error(), "Cannot add existing keyspace" ) == 0 {
			return nil 
		}
		dc.sheep.Baa( 1, "keyspace create returned: %s", err )
	} else {
		dc.sheep.Baa( 2, "keyspace exists in datacache" )
	}

	return err
}

/*
	Ensure that our required tables are in the cache.  There has to be a better way than this, 
	but maybe not. 
*/
func ( dc *Dcache ) ensure_tables() ( err error ) {
	
	err  = dc.sess.Query( fmt.Sprintf( `CREATE TABLE ulcaps ( project text PRIMARY KEY, pctg int)` ) ).Exec()
	if err != nil {
		if strings.Index( err.Error( ), "Cannot add already existing table" ) != 0 {
			dc.sheep.Baa( 1, "ulcaps table create failed: %s", err )
			return err
		}
	} 
	dc.sheep.Baa( 1, "ulcap table exists" )

	err  = dc.sess.Query( fmt.Sprintf( `CREATE TABLE endpts ( epid text PRIMARY KEY, epdata map<text,text> )` ) ).Exec()
	if err != nil {
		if strings.Index( err.Error( ), "Cannot add already existing table" ) != 0 {
			dc.sheep.Baa( 1, "endpts table create failed: %s", err )
			return err
		}
	} 
	dc.sheep.Baa( 1, "endpts table exists" )

		//chkpt = fmt.Sprintf( `{ 

	err  = dc.sess.Query( fmt.Sprintf( `CREATE TABLE bwres ( resid text PRIMARY KEY, expiry int, project text, resdata map<text,text> )` ) ).Exec()
	if err != nil {
		if strings.Index( err.Error( ), "Cannot add already existing table" ) != 0 {
			dc.sheep.Baa( 1, "bwres table create failed: %s", err )
			return err
		}
	} 
	dc.sheep.Baa( 1, "bwres table exists" )

	err  = dc.sess.Query( fmt.Sprintf( `CREATE TABLE bwowres ( resid text PRIMARY KEY, expiry int, project text, resdata map<text,text> )` ) ).Exec()
	if err != nil {
		if strings.Index( err.Error( ), "Cannot add already existing table" ) != 0 {
			dc.sheep.Baa( 1, "bwowres table create failed: %s", err )
			return err
		}
	} 
	dc.sheep.Baa( 1, "bwowres table exists" )

	err  = dc.sess.Query( fmt.Sprintf( `CREATE TABLE mirres ( resid text PRIMARY KEY, expiry int, project text, resdata map<text,text> )` ) ).Exec()
	if err != nil {
		if strings.Index( err.Error( ), "Cannot add already existing table" ) != 0 {
			dc.sheep.Baa( 1, "mirres table create failed: %s", err )
			return err
		}
	} 
	dc.sheep.Baa( 1, "mirres table exists" )

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

//------ generic access functions (internal) ---------------------------------------------------------------------------

/*
	A generic get one map (a field stored as a map) from the datacache function. Parms:
		dataname - the field to pull from the matching record
		keyname - the field name that is used (where keyname =
		keyval -  the value of the key name (where keyname = keyval)
		table - the table name in our space
		target - a pointer to the struct to populate with the data.

	err is nil on return when succcessful.

*/
func ( dc *Dcache ) get_one_map( table string, keyname string, keyvalue string, dataname string, target interface{} ) ( err error ) {
	var (
		rdata map[string]string				// stuff that comes back from the datacache is a map
	)

	if dc == nil {
		return fmt.Errorf( "no struct passed to get_one_res" )
	}

	if !dc.connected {
		if  ok, err := dc.connect(); ! ok {
			return err
		}
	}

	err = dc.sess.Query( fmt.Sprintf( `SELECT %s FROM %s WHERE %s = ? LIMIT 1`, dataname, table, keyname ), keyvalue ).Consistency(gocql.One).Scan( &rdata )
    if err != nil {
		msg := fmt.Sprintf( "dcache/get_one_map: unable to find field %s for %s=%s in %s: %s ", dataname, keyname, keyvalue, table, err )
		dc.sheep.Baa( 2, msg )
		err = fmt.Errorf( msg )
		return  err
    } else {
		dc.sheep.Baa( 1, "found field %s for %s=%s in %s", dataname, keyname, keyvalue, table )
	}

	
	transform.Map_to_struct( rdata, target, "dcache" )		// transform the map into the struct

	return err
}

