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

	Mnemonic:	rjprt.go
	Abstract:	Send an http oriented post or get request that results in json output and
				then format the output in a human readable fashion to stdout.
				is returned.  Input command line:
					-a token	Authorisation token related to the privlege of executing the command
								such as listhost (not VM related tokens for a reservation).
					-d 			Display result in 'dotted' notaion rather than indented hiarchy
					-D			POST request "body of data"
					-j			No formatting, outputs raw json
					-J			Send application/json as the type rather than application/text.
					-l	path	implies -d; looks for the 'path' in the result (e.g. root[0].mac[0])
					-m method	where method is POST or GET (GET is default)
					-r			returns raw json (debugging mostly)
					-t url		Target url
					-T			tight security (does not trust host certificates that cannot be validated)
					-v			verbose

	Date:		01 January 2014
	Author:		E. Scott Daniels

	Mod:		07 Jun 2014 - Added ability to ignore invalid certs (-T option added to force tight security)
				09 Jul 3014 - Added -J capability
				14 Dec 2014 - Formally moved to tegu code.
				03 Jun 2015 - To accept an authorisation token to send as X-Tegu-Auth in the header.
				19 Jun 2015 - Added support to dump headers in order to parse out the token that openstack
					identity v3 sends back in the bloody header of all places.
				24 Jun 2015 - Added xauth support to GET.
*/

package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/att/gopkgs/jsontools"
)

// global variables
var (
)

func usage( version string ) {
	fmt.Printf( "%s\n", version );
	fmt.Printf( "usage: rjprt [-a auth-string] [-D data-string] [-d] [-t target-url] [-j] [-J] [-l dot-string] [-m GET|POST|DELETE] [-r root-string] [-T] [-v] -t target url\n" )
	fmt.Printf( `	If -m POST or -m DELETE is supplied, and -D "string" is omitted, rjprt will read the POST data from stdin` )
	fmt.Printf( "\n\n" )
}

func main() {
	var (
		version		string = "rjprt v1.4/16195"
		auth		*string
		err			error
		resp		*http.Response
		verbose 	*bool
		root		*string
		raw_json 	*bool
		needs_help *bool
		target_url	*string
		dot_fmt 	*bool
		look4 		*string
		method		*string
		request_data	*string
		req			*http.Request
	)

	needs_help = flag.Bool( "?", false, "show usage" )

	auth = flag.String( "a", "", "authorisation token" )
	dot_fmt = flag.Bool( "d", false, "dotted named output" )
	request_data = flag.String( "D", "", "post data" )
	raw_json = flag.Bool( "j", false, "raw-json" )
	show_headers := flag.Bool( "h", false, "show http response headers" )
	appl_json := flag.Bool( "J", false, "data type is json" )
	look4 = flag.String( "l", "", "look4 string in hashtab" )
	method = flag.String( "m", "GET", "request method" )
	root = flag.String( "r", "", "top level root name" )
	target_url = flag.String( "t", "", "target url" )
	tight_sec := flag.Bool( "T", false, "tight security ON" )
	verbose = flag.Bool( "v", false, "verbose" )
	flag.Parse();									// actually parse the commandline

	input_type := "text"			// type of data being sent in body
	if *appl_json {
		input_type = "application/json"
	}

	if *needs_help {
		usage( version )
		os.Exit( 0 )
	}

	if *look4 != "" {				// -l imples -d
		*dot_fmt = true
	}

	if *target_url == "" {
		fmt.Fprintf( os.Stderr, "target url is missing from command line (use -t url)\n" )
		os.Exit( 1 )
	}

	if( *verbose ) {
		fmt.Fprintf( os.Stderr, "target=%s\n", *target_url )
	}

	trparms := &http.Transport{				// override default transport parms to skip the verify
        TLSClientConfig: &tls.Config{ InsecureSkipVerify: !*tight_sec },
    }

	client := &http.Client{ Transport: trparms }		// default client except with our parms
	
	switch( *method ) {
		case "GET", "get":
			req, err = http.NewRequest( "GET", *target_url, nil )
			if err == nil {
				if auth != nil && *auth != "" {
					req.Header.Set( "X-Auth-Tegu", *auth )
				}
	
				resp, err = client.Do( req )
			}

		case "POST", "post":
			if *request_data == "" {
				req, err = http.NewRequest( "POST", *target_url, os.Stdin )
			} else {
				req, err = http.NewRequest( "POST", *target_url, bytes.NewBufferString( *request_data ) )
			}

			if err == nil {
				req.Header.Set( "Content-Type", input_type )
				if auth != nil && *auth != "" {
					req.Header.Set( "X-Auth-Tegu", *auth )
				}
	
				resp, err = client.Do( req )
			}

		case "DELETE", "del", "delete":
			if *request_data == "" {
				req, err = http.NewRequest( "DELETE", *target_url, os.Stdin )
			} else {
				req, err = http.NewRequest( "DELETE", *target_url, bytes.NewBufferString( *request_data ) )
			}

			if err == nil {
				if auth != nil && *auth != "" {
				req.Header.Add( "X-Auth-Tegu", *auth )
				}
				resp, err = client.Do( req )
			}

		default:
			fmt.Fprintf( os.Stderr, "%s method is not supported\n", *method )
			os.Exit( 1 )
	}

	if err != nil {
		fmt.Printf( "%s request failed: %s\n", *method, err )
		os.Exit( 1 )
	} else {
		data, err := ioutil.ReadAll( resp.Body )
		if err != nil {
			fmt.Printf( "read of data from url failed\n" )
			os.Exit( 1 )
		}
		resp.Body.Close( )

		if *show_headers {
			for k, v := range resp.Header {
				fmt.Printf( "header: %s = %s\n", k, v )
			}
		}

		if data == nil {
			os.Exit( 0 );					// maybe not what they were expecting, but nothing isn't an error
		}

		if *raw_json {
			fmt.Printf( "%s\n", data )
			os.Exit( 0 )
		}

		if *dot_fmt {
			m, err := jsontools.Json2map(  data, root, false );			// build the map
			if err == nil {
				if *look4 != "" {
					result := m[*look4]
					if result != nil {
						switch result.( type ) {
							case string:
								fmt.Printf( "%s = %s\n", *look4, result.(string) )

							case int:
								fmt.Printf( "%s = %d\n", *look4, result.(int) )

							case float64:
								fmt.Printf( "%s = %.2f\n", *look4, result.(float64) )

							default:
								fmt.Printf( "found %s, but its in an unprintable format\n", *look4 )
						}

					} else {
						fmt.Fprintf( os.Stderr, "didn't find: %s\n", *look4 )
					}
				} else {
					jsontools.Print( m, *root, true )
				}
			} else {
				fmt.Fprintf( os.Stderr, "ERR: %s \n", err );
			}
		} else {
			_, err = jsontools.Json2blob( data, root, true );			// normal hiarchy can be printed as it blobs, so ignore jif coming back
			if  err != nil {											// assume mirroring which doesn't put out json in all cases (boo)
				//fmt.Fprintf( os.Stderr, "ERR: %s \n", err );
				fmt.Fprintf( os.Stdout, "%s\n", data );
			}
		}
	}

	os.Exit( 0 )
}

