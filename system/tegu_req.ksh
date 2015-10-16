#!/usr/bin/env ksh
# vi: sw=4 ts=4:
#
# ---------------------------------------------------------------------------
#   Copyright (c) 2013-2015 AT&T Intellectual Property
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at:
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
# ---------------------------------------------------------------------------
#

#
#	Mnemonic:	tegu_req.ksh
#	Abstract: 	Request interface to tegu. This is a convenience and scripts can just use
#				a direct curl if need be.  This script uses rjprt to make the request and
#				then to print the resulting json in a more human friendly manner.  Using the
#				-j optin causes the raw json returned by Tegu to be spilled to standard
#				out.
#
#				Looking at the code and wondering what `rjprt` is?  Well it's short for
#				request and json print. It acts (somewhat) like curl in as much as it
#				makes a GET/PUT/POST/DELETE request supplying the body of data to be worked
#				on. Then, unlike curl, formats the resulting json output before writing it
#				to the tty device. The -j and -d options control the format of the output.
#
#				NOTE: this script was dumbed down to work with bash; it may still not
#					function correctly with bash and before complaining just install ksh
#					and use that.
#
#	Date:		01 Jan 2014
#	Author:		E. Scott Daniels
#
#	Mods:		05 Jan 2014 - Added listres and cancel support.
#				08 Jan 2014 - Changed the order of parms on the reservation command so
#					that they match the order on the HTTP interface. This was purely
#					to avoid confusion.  Added a bit better error checking to reserve
#					and consistently routing error messages to stderr.
#				03 Mar 2014 - Added error checking based on host pair syntax.
#				31 Mar 2014 - Allows for queue dump/list command.
#				13 May 2014 - Added support to provide dscp value on a reservation req.
#				09 Jun 2014 - Added https proto support.
#				17 Jun 2014 - Added token authorisation support for privileged commands.
#				22 Jul 2014 - Added support for listulcap request.
#				09 Nov 2014 - change <<- here doc command to << -- seems some flavours of
#					kshell don't handle this?  Also removed support for keystone cli
#					and substituted a curl command since it seems keystone isn't installed
#					everyplace (sigh).
#				25 Feb 2015 - Added mirror commands
#				31 Mar 2015 - Added support for sending key-value pairs to listhosts and
#					graph commands.
#				01 Apr 2015 - Corrected bug with token passed on steering request.
#				18 May 2015 - Dumbed down so that bash could run the script.
#				02 Jun 2015 - Added optional request name to *-mirror commands to make
#					consistent with others (no dash).
#				04 Jun 2015 - Added token to -a call.
#				10 Jun 2015 - Added one way reservation support
#				19 Jun 2015 - Added support for v3 token generation.
#				30 Jun 2015 - Fixed a bunch of typos.
#				01 Jul 2015 - Correct bug in mirror timewindow parsing.
#				20 Jul 2015 - Corrected potential bug with v2/3 selection.
#				22 Sep 2015 - Added support to suss out our URL from the keystaone catalogue 
#					and use that if it's there. Corrected dscp value check (was numeric).
#					Added ability to use %p in endpoint name for project (pulls from OS_ var).
#				15 Oct 2015 - Changes to support endpoint uuid requirement as a part of the hostname.
# ----------------------------------------------------------------------------------------

function usage {
	cat <<endKat

	version 2.0/19225
	usage: $argv0 [-c] [-d] [-f] [-h tegu-host[:port] [-j] [-K] [-k key=value] [-R region] [-r rname] [-S service] [-s] [-t token|-T] command parms

	  -c do not attempt to find our URL from the keystone catalogue. (see -h)
	  -d causes json output from tegu to be formatted in a dotted hierarch style
	  -f force prompting for user and password if -T is used even if a user name or password is
	     currrently set in the environment.
	  -h is needed when tegu is running on a different host than is being used to run tegu_req
	     and/or when tegu is listening on a port that isn't the default. Implies -c.
	  -j causes raw json to be spilled to the standard out device
	  -k k=v Supplies a key/value pair that is necessary on some requests. Multiple -k options
	     can be supplied when needed.
	  -K Use keystone command line interface, rather than direct API, to generate a token
	     (ignored unless -T is used)
	  -R name  Allows a region name to be supplied to override the OS_REGION environment variable.
	     If OS_REGION is not defined, and this option is not given, the the first region in the 
	     list returned by Openstack will be used (specify the region if you must be sure!).
	  -r allows a 'root' name to be supplied for the json output when humanised
	  -S name  Overrides the QL_SERVICE name defined in the environment and uses name as
	     the lookup key when sussing through the catalogue for the URL to use for Tegu.
	     If -S not supplied, and QL_SERVICE not defined, then netqos is assumed.
	  -s enables secure TLS (https://) protocol for requests to Tegu.
	  -t allows a keystone token to be supplied for privileged commands; -T causes a token to
	     be generated using the various OS_ environment variables. If a needed variable is
	     not in the environment, then a prompt will be issued. When either -t or -T is given
	     a %t can be used on the commandline in place of the token and the token will
	     substituted. For example: %t/cloudqos/daniels8  would substitute the generated
	     token into the host name specification.  Similarly, %p can be used on the command line
	     to cause the project name (assuming OS_TENANT_NAME is set) to be substituted.

	commands and parms are one of the following:
	  $argv0 reserve [bandwidth_in,]bandwidth_out [start-]expiry host1,host2 cookie [dscp]
	  $argv0 owreserve bandwidth_out [start-]expiry token/project/host1,token/project/host2 cookie [dscp]
	  $argv0 cancel reservation-id [cookie]
	  $argv0 listconns {name[ name]... | <file}
	  $argv0 add-mirror [start-]end port1[,port2...] output [cookie] [vlan]
	  $argv0 del-mirror name [cookie]
	  $argv0 list-mirrors
	  $argv0 show-mirror name [cookie]

	Privileged commands (admin token must be supplied)
	  $argv0 graph
	  $argv0 listhosts
	  $argv0 listulcap
	  $argv0 listres
	  $argv0 listqueue
	  $argv0 setdiscount value
	  $argv0 setulcap tenant percentage
	  $argv0 refresh hostname
	  $argv0 steer  {[start-]end|+seconds} tenant src-host dest-host mbox-list cookie
	  $argv0 verbose level [subsystem]

	  Host specifiers:  host1 and host2, where applicable, are specified in one of
	  the following formats:
	      token/project/vm-name
	      token/project/[vm-name@]ipv4-address[:port]
	      token/project/[vm-name@]ipv6-address[:port]
	      token/project/endpoint-uuid/ipv4-address[:port]
	      token/project/endpoint-uuid/ipv6-address[:port]

	  Tegu requires endpoint (interface) uuids and tegu_req provides a translation
	  capability which will convert the "old style" token/project/vm-name style.
	  When vm-name alone is supplied, tegu_req attempts to look the name up and will 
	  use the first interface that it finds as the endpoint.  This is fine if the VM
	  has only one interface, but probably isn't desired when the VM has multiple 
	  interfaces. The vm-name@address form yields a faster lookup (2 seconds vs 8 seconds
	  in the lab), and should be used over supplying just an ip address alone. If the
	  interface endpoint UUID is known, that format doesn't require any lookup and 
	  thus is the fastest to resolve. 

	  IPv6 addresses must always be contained in square braces (e.g. [fe81::257], 
	  [fe81::257]:80)
 
	  If only bandwidth_out is supplied, then that amount of bandwidth is reserved
	  in each direction. Otherwise, the bandwidth out value is used to reserve
	  bandwidth from host1 (out) to host2 and the bandwidth in is used to reserve
	  bandwidth from host2 (in) to host1. Both values may be specified with trailing
	  G/M/K suffixes (e.g. 10M,20M).

	  The dscp value is one of three strings, (voice, data, control) with an optional
	  global_ as a prefix.  This causes the reserved traffic to be marked with one 
	  of the three ITONs values.  Adding global_ causes the marking to be left
	  on the traffic as it passes out of the cloud environment.

	  For the listconns command, "name" may be a VM name, VM ID, or IP address. If
	  a file is supplied on stdin, then it is assumed to consist of one name per
	  line.

	  For the cancel command the reservation ID is the ID returned when the reservation
	  was accepted.  The cookie must be the same cookie used to create the reservation
	  or must be omitted if the reservation was not created with a cookie.

	  For verbose, this controls the amount of information that is written to the log
	  (stderr) by Tegu.  Values may range from 0 to 9. Supplying the subsystem causes
	  the verbosity level to be applied just to the named subsystem.  Subsystems are:
	  net, resmgr, fqmgr, http, agent, fqmgr, or tegu

	Admin Token
	  The admin token can be supplied using the -t parameter and is required for all
	  privileged commands. The token can be generated by invoking the keystone token-get
	  command for the user that is defined as the admin in the Tegu configuration file.
	  The admin token is NOT the token that is defined in the Openstack configuration.
	  If the -T option is used, $argv0 will prompt for username and password and then
	  will generate the admin token to use.   Tokens may be needed on host names
	  and those must be generated independently.

endKat
}

function cleanup
{
	trap - 1 2 3 15 EXIT
	stty echo
	rm -f /tmp/PID$$.*
}

function verbose
{
	if (( vlevel > 0 ))
	then
		echo "$1" >&2
	fi
}

#
# Accepts a vm name, or ip address, and converts it to an interface uuid for the endpoint.
# The openstack digger accepts multiple names/addresses to translate, but to use this
# it requires a bunch of calls to openstack that produce no useful results. The fastest
# way to translate this information (short of caching a local copy) is to hope the user
# gave us the form vmname@ipaddress[:port] rather than an ip address.  This way, we can 
# set the limit flag on the digger call which reduces the interface query to just one
# api call. If the user gives us just IP address(es) then the call will take time. 
# To run as quickly as possible we send individually limited calls for any host name we
# can and a single batch call of the rest.  The batch calls take about 8 seconds on a 
# test lab, so likely much longer in a real environment, while the limited call takes about 
# 2 seconds in the same lab.  Typically, we'll be resolving at most 2 addresses, and if one
# is an 'external' address (!//ip), then there is no call made for those.
#
# Using just a vm name (no ip address) is potentially disastrous (when the vm has more 
# than one interface), but # for backward compatability, and convenience, we'll support that too.
#
# The string passed in better be of the form:  token/project/address|name. Output will be:
# token/project/endpoint/address.
# Sadly, openstack look-ups are _expensive_ (8 seconds on a tiny lab system), so we
# assume that we'll be passed a bunch of names (all that need to be converted) and we'll
# make one call to tegu_osdig to make one openstack requests.
# For external addresses (!//ip) we expect the ip not to match in openstack and will 
# generate !///ip which is what tegu expects.
function name2epid
{
	if [[ ${1%%,*} == *"/"*"/"*"/"* ]]			# already in token/p/e/a format?
	then
		echo "$@"								# assume they all are, and just write them back
		exit
	fi

	typeset i=0
	typeset -A prefixes
	typeset -A names
	typeset lookup=""
	typeset limiter=""
	typeset result=""

	while [[ -n $1 ]]
	do
		typeset prefix="${1%/*}"
		typeset name="${1##*/}"

		if [[ $name == *"@"* ]]			# if name@address given, we can make a quicker dig call in the loop
		then
			limiter="-l ${name%%@*}"	# limit the dig to just this VM name (faster)
			name="${name#*@}"			# vm name is _not_ kept
		else
			limiter=""					# no vm name: we have to add this to the list that will be batched at the end
		fi

		case $name in 					# need to strip :port, and [/] if ipv6
			\[*) 
				typeset x="${name%%]*}"
				this_lookup="${x#*\[}"
				v6_flag="ipv6=true"		# needs to be supplied to tegu so flow-mods are marked for v6
				;;	

			*:*) this_lookup="${name%:*} " ;;

			*)	 this_lookup="$name ";;
		esac

		this_lookup="${this_lookup%%\{*}"		# ditch any vlan specification that might trail the address

		if [[ $prefix == "!"* ]]
		then
			result="$result${prefix}//${name} "
		else
			if [[ -z $limiter ]]			# could not limit this one, will have to do in batch later (slower)
			then
				prefixes[$i]="$prefix"
				names[$i]="$name"
	
				(( i++ ))
				lookup="$lookup$this_lookup "	# add to batch we'll need to do at the end
			else
				x=$( tegu_osdig $limiter epid $this_lookup )
				result="$result${prefix}/${x}/${name} "
			fi
		fi

		shift
	done

	if [[ -n $lookup ]]						# one last call to dig any that could not be limited by vm name
	then
		i=0
		tegu_osdig -v epid $lookup |while read x
		do
			x=${x##* }											# -v ensures we get one for each with x: missing if not found
			result="$result${prefixes[$i]}/${x/missing/}/${names[$i]} "
	
			(( i++ ))
		done
	fi

	echo $result
}

# takes a string $3 and makes substitutions such that %t is replaced with $1 and %p is 
# replaced with $2
function expand_epname
{
	typeset s="${3//%t/$1}"
	s="${s//%p/$2}"
	echo "$s"
}

# generate the input json needed to request a token using openstack/keystone v3 interface
function gen_v3_token_json {
cat <<endKat
{
 "auth": {
   "identity": {
     "methods": [ "password" ],
     "password": {
       "user": {
       		"domain": { "name": "${OS_DOMAIN_NAME:-default}" },
			"name": "${OS_USERNAME:-missing}", "password": "${OS_PASSWORD:-missing}"
	   }
     },
   "scope": {
     "project": {
       "name": "$OS_TENANT_NAME"
     }
   }
  }
 }
}
endKat
}

function ensure_osvars
# ensure that the needed OS variables are set; prompt for them if not
{
	typeset token_value=""
	typeset xOS_PASSWORD=""
	typeset xOS_USERNAME=""
	typeset xOS_TENANT_NAME=""

	if [[ -z $OS_USERNAME ]]
	then
		printf "Token generation:\n\tEnter user name: " >/dev/tty
		read xOS_USERNAME
		OS_USERNAME="${xOS_USERNAME:-nonegiven}"
	fi

	if [[ -z $OS_PASSWORD ]]
	then
		default="no-default"

		printf "\tEnter password for $OS_USERNAME: " >/dev/tty
		stty -echo
		read xOS_PASSWORD
		stty echo
		printf "\n" >/dev/tty

		OS_PASSWORD=${xOS_PASSWORD:-nonegiven999}
	fi

	if [[ -z $OS_TENANT_NAME ]]
	then
		printf "\tEnter tenant: " >/dev/tty
		read OS_TENANT_NAME
	fi

	if [[ -z $OS_AUTH_URL ]]
	then
		printf "\tEnter keystone url: " >/dev/tty
		read OS_AUTH_URL
	fi

	if [[ -z $region ]]
	then
		printf "\tEnter region: " >/dev/tty
		read region
	fi
}

# Parse output from a keystone token request to suss out the URL for tegu.
# We assume that the service name is QL_SERVICE in the environment or netqos by
# default.  We depend on rjprt as we use the dotted output format which makes
# it fairly straight forward to parse.  Echo nothing from this function as the
# url is written to stdout for the caller.  As a side effect of this funciton we
# do get the user's token
function suss_url
{
	ensure_osvars
	export OS_USERNAME
	export OS_TENANT_NAME
	export OS_AUTH_URL

	typeset	url="$OS_AUTH_URL/tokens"
	rjprt -r x -d -J -t  $url -m POST -D "{\"auth\": {\"tenantName\": \"${OS_TENANT_NAME:-garbage}\", \"passwordCredentials\": {\"username\": \"$OS_USERNAME\", \"password\": \"${OS_PASSWORD:-garbage}\" }}}" | awk \
		-v url_type="$url_type" \
		-v region=${region:-any}  \
		-v sname="${1:-none}" \
		-v tfname="/tmp/PID$$.tk" \
	'
	function strip( src ) {
		while( substr( src, 1, 1 ) == " " )
			src = substr( src, 2 )
		return src
	}

	{
		split( $1, a, "." )
		if( a[2] == "access" && substr( a[3], 1, 14 )  == "serviceCatalog" ) {
			split( a[3], b, "[" )
			if( b[2] == "len" )
				next;

			idx = b[2]+0			# [len] will map to 0
	
			if( index( a[4], "type" ) > 0  ) {
				cat_type[idx] = strip( $NF );
			} else {
				if( index( a[4], "name" ) > 0  ) {
					cat_name[idx] = strip( $NF );
					cat_len++;
				} else {
					if( substr( a[4], 1, 5 ) == "endpo" ) {
						split( a[4], b, "[" );
						if( b[2] != "len" ) {
							ep_idx = b[2] + 0;
		
							if( index( a[5], url_type ) > 0  ) {
								ep_url[idx,ep_idx] = strip( $NF );
							} else {
								if( index( a[5], "region" ) > 0 ) {
									ep_reg[idx,ep_idx] = strip( $NF );
									ep_len[idx]++;
								}
							}
						}
					}
				}
			}
		} else {
			if( a[2] == "access" && a[3] == "token" && a[4] == "id" ) {
				if( ! have_token ) {
					have_token = 1;
					printf( "%s\n", $NF ) >tfname			# save token since bash incapable of reading two values from a function
				}
			}
		}

		next;
	}

	END {
		for( i = 0; i < cat_len; i++ ) {			# for each catalogue type
			if( cat_name[i] == sname ) {
				for( j = 0; j < ep_len[i]; j++ ) {
					if( region == "any" || ep_reg[i,j] == region ) {
						printf( "%s\n", ep_url[i,j] );
						exit( 0 );
					}
				}
			}
		}

		exit( 1 )
	}
	'		# end
}

# parse the output from keystone/openstack version2 token generation
function v2_suss_token {
	awk '{ print $0 "},"} ' RS="," | awk '1' RS="{" | awk '
		/"access":/ { snarf = 1; next }				# we want the id that is a part of the access struct
		/"id":/ && snarf == 1  {					# so only chase id if we have seen access tag
			gsub( "\"", "", $0 );					# drop annoying bits of json
			gsub( "}", "", $0 );
			gsub( ",", "", $0 );
			print $NF
			exit ( 0 );								# stop short; only need one
		} '											# now bash compatible
}

# Run the v3 output for the returned token
# Bloody openstack puts the token from a v3 request in the HEADER and not in the body
# with the rest of the data; data does NOT belong in the transport header! Header fields
# are tagged by rjprt and are contained in square brackets which need to be stripped.
function v3_suss_token {
	awk '/header: X-Subject-Token/ { gsub( "\\[", "", $NF ); gsub( "]", "", $NF ); print $NF; exit( 0 ); }'
}

function str2expiry
{
	typeset expiry
	if [[ $1 == "+"* ]]
	then
		expiry=$(( $(date +%s) $1 ))
	else
		if [[ $1 == -* ]]
		then
			echo "start-end timestamp seems wrong: $2  [FAIL]" >&2
			usage >&2
			exit 1
		fi

		expiry=$1
	fi

	echo $expiry
}

# given a raw token, or nothing, generate the proper rjprt option to set
# it in the header.
# CAUTION: error messages MUST go to &2
function set_xauth
{
	if [[ -n $1 ]]
	then
		if ! rjprt -?|grep -q -- -a
		then
			echo "" >&2
			echo "WARNING: the version of rjprt installed in $(which rjprt) is old, some information might not be sent to tegu" >&2
			echo "         install a new version of rjprt, or remove the old one" >&2
			echo "" >&2
		fi

		echo " -a $1 "
	fi
}


function gen_token
{
	if [[ -s /tmp/PID$$.tk ]]		# token generated during catalogue url search
	then
		token_value=$( </tmp/PID$$.tk  )		# just pull what we have and use it
		rm /tmp/PID$$.tk						# don't wait for end, trash now
		echo "$token_value"
		return
	fi

	ensure_osvars

	export OS_TENANT_NAME
	export OS_PASSWORD
	export OS_USERNAME
	export OS_AUTH_URL

	if (( use_keystone ))			# -K used on the command line
	then
		token_value=$( keystone token-get | awk -F \| '{gsub( "[ \t]", "", $2 ) } $2 == "id" {print $3 }' )	# now bash compatible
	else
		content_type="Content-type: application/json"
		case $OS_AUTH_URL in
			 */v2.0*)
				url="$OS_AUTH_URL/tokens"
				token_value=$( curl -s -d "{\"auth\":{ \"tenantName\": \"$OS_TENANT_NAME\", \"passwordCredentials\":{\"username\": \"$OS_USERNAME\", \"password\": \"$OS_PASSWORD\"}}}" -H "$content_type" $url  | v2_suss_token )
				;;

			*/v3*)
				url="$OS_AUTH_URL/auth/tokens"
				body="$( gen_v3_token_json )"			# body for the url
				token_value=$( rjprt -h -J -m POST -d -D "$body" -t $url | v3_suss_token )
				;;

			*)	echo "version in OS_AUTH_URL ($OS_AUTH_URL) is not supported for -T" >&2
				exit 1
				;;
		esac

	fi

	if [[ -z $token_value ]]
	then
		echo "unable to generate a token for $OS_USERNAME    [FAIL]" >&2
		return 1
	fi

	echo ${token_value%% *}					# ensure any trailing junk is gone
	return 0
}

# ------------------------------------------------------------------------------------------------------------

trap cleanup 1 2 3 15 EXIT		# ensure tty is restored to sane state, and cleanup scratch files in /tmp

argv0="${0##*/}"
port=29444
host=localhost:$port
opts=""
root=""
proto="http://"
prompt4token=0
force=0
use_keystone=0
url_type="publicURL"
skip_catalogue=0			# -c causes this to set and we don't look in keystone catalogue for our url/port
vlevel=0
region=${OS_REGION:=any}	# if not supplied, default to first region (any)

bandwidth="tegu/bandwidth"		# http api collections
steering="tegu/api"				# eventually this should become steering
default="tegu/api"
force_endpoints=1				# for backwards compatability -e will set/clear based on this default

while [[ $1 == -* ]]
do
	case $1 in
		-c)		skip_catalogue=1;;						# allow for default host (locahost)
		-d)		opts+=" -d";;
		-e)		force_endpoints=$(( abs( force_endpoints - 1 ) ));;
		-f)		force=1;;
		-F)		bandwidth="api"; steering="api";;		# force collection to old single set style
		-h) 	skip_catalogue=1; host=$2; shift;;
		-j)		opts+=" -j";;
		-k)		kv_pairs+="$2 "; shift;;
		-K)		use_keystone=1;;
		-R)		region=$2; shift;;
		-r)		root="$2"; shift;;
		-s)		proto="https://";;
		-S)		QL_SERVICE=$2; shift;;					# override environment
		-t)		raw_token="$2"; token=$"auth=$2"; shift;;
		-T)		prompt4token=1;;
		-v)		(( vlevel++ ));;
		-\?)	usage
				exit 1
				;;

		*)		echo "ERROR: unrecognised option: $1"
				usage
				exit 1
				;;
	esac

	shift
done

ql_service="${QL_SERVICE:-netqos}"			# use environment or default

opts+=" -r ${root:-$1}"

if (( force > 0 ))							# force username and password prompts; other OS vars default if set
then
	OS_USERNAME=""
	OS_PASSWORD=""
fi

if (( ! skip_catalogue ))
then
	port="443"										# assume some kind of proxy with this default port
	cat_host=$( suss_url $ql_service )	# suss out the url for our service
	if [[ -z $cat_host ]]
	then
		verbose "WARNING: unable to find  $ql_service in service catalogue; using $host"
	else
		proto=""							# assume proto comes from catalogue
		host="$cat_host"
		verbose "using url from service catalogue: $host"

		# NOTE: the conditional statement below  is a temporary hack until AICv2 is installed and the requirement for
		# the 'tag' to be netqos goes away if it's an old system the ping will fail and we need to replace tegu/api
		# etc. with netqos in the url.
		if  ! rjprt  $opts -m POST -t "$proto$host/$default " -D "$token ping" 2>/dev/null |grep -q pong
		then
			default=netqos
			steering=netqos
			bandwidth=netqos
		fi

	fi
fi

if [[ $host != *":"* ]]
then
	host+=":$port"
fi

if (( prompt4token ))						# if -T given, prompt for information needed to generate a token
then
	raw_token="$( gen_token )"
	if [[ -z $raw_token ]]
	then
		exit 1
	fi
	token="auth=$raw_token"
fi


opts+=$( set_xauth $raw_token )
case $1 in
	ping)
		rjprt  $opts -m POST -t "$proto$host/$default" -D "$token ping"
		;;

	listq*|qdump|dumpqueue*)
		rjprt  $opts -m POST -t "$proto$host/$bandwidth" -D "$token qdump"
		;;

	listr*)
		rjprt  $opts -m POST -t "$proto$host/$default" -D "$token listres $kv_pairs"
		;;

	listh*)						# list hosts
		rjprt  $opts -m POST -t "$proto$host/$default" -D "$token listhosts $kv_pairs"
		;;

	listul*)						# list user link caps
		rjprt  $opts -m POST -t "$proto$host/$bandwidth" -D "$token listulcaps"
		;;

	listc*)						# list connections
		if (( $# < 2 ))			# assume it's on stdin
		then
			sed 's/^/listconns /' >/tmp/PID$$.data
		else
			shift
			for x in "$@"
			do
				echo "listconns $x"
			done >/tmp/PID$$.data
		fi

		rjprt  $opts -m POST -t "$proto$host/$default" </tmp/PID$$.data
		rm -f /tmp/PID$$.data
		;;

	graph)
		rjprt  $opts -m POST -D "$token graph $kv_pairs" -t "$proto$host/$default"
		;;


	cancel)
		shift
		case $# in
			1|2) ;;
			*)	echo "bad number of positional parameters for cancel [FAIL]" >&2
				usage >&2
				exit 1
				;;
		esac

		rjprt $opts -m DELETE -D "reservation $1 $2" -t "$proto$host/$bandwidth"
		;;

	pause)
		rjprt $opts -m POST -D "$token pause" -t "$proto$host/$default"
		;;

	refresh)
		rjprt  $opts -m POST -D "$token refresh $2" -t "$proto$host/$default"
		;;

	resume)
		rjprt $opts -m POST -D "$token resume" -t "$proto$host/$default"
		;;

	reserve)
		shift
		#tegu command is: reserve <bandwidth>[K|M|G] [<start>-]<end>  <host1-host2> [cookie [dscp]]
		if (( $# < 4 ))
		then
			echo "bad number of positional parms for reserve  [FAIL]" >&2
			usage >&2
			exit 1
		fi

		expiry=$( str2expiry $2 )
		if [[ $3 != *"-"* ]] && [[ $3 != *","* ]]
		then
			echo "host pair must be specified as host1-host2 OR host1,host2   [FAIL]" >&2
			exit 1
		fi
		if [[ -n $5 ]]
		then
			case $5 in
				voice|data|control) ;;			# valid
				global_voice|global_data|global_control) ;;  # valid
				
				*)
						echo "dscp value ($5) must be one of [global_]voice, [global_]data or [global_]control  [FAIL]" >&2
						exit 1
						;;
			esac
		fi
		#rjprt  $opts -m POST -D "reserve $kv_pairs $1 $expiry ${3//%t/$raw_token} $4 $5" -t "$proto$host/$bandwidth"
		#rjprt  $opts -m POST -D "reserve $kv_pairs $1 $expiry $(expand_epname "$raw_token" "$OS_TENANT_NAME" $3) $4 $5" -t "$proto$host/$bandwidth"
		if (( force_endpoints ))
		then
			hosts=$(expand_epname "$raw_token" "$OS_TENANT_NAME" $3) 			# expand token/project bugs
			hosts=$( name2epid ${hosts//,/ } )									# look up endpoint uuid and add
			hosts="${hosts// /,}"												# tegu requires a single token, comma sep; make it so
		else
			hosts=$(expand_epname "$raw_token" "$OS_TENANT_NAME" $3) 
		fi

		rjprt  $opts -m POST -D "reserve $v6_flag $kv_pairs $1 $expiry $hosts $4 $5" -t "$proto$host/$bandwidth"
		;;

	owres*|ow_res*)
		shift
			#tegu command is: owreserve <bandwidth>[K|M|G] [<start>-]<end>  <host1,host2> [cookie [dscp]]
		if (( $# < 4 ))
		then
			echo "bad number of positional parms for owreserve  [FAIL]" >&2
			usage >&2
			exit 1
		fi

		if (( force_endpoints ))												# must ensure that endpoints are added to the host string(s)
		then
			hosts=$(expand_epname "$raw_token" "$OS_TENANT_NAME" $3) 			# expand token/project bugs
			hosts=$( name2epid ${hosts//,/ } )									# look up endpoint uuid and add
			hosts="${hosts// /,}"												# tegu requires a single token, comma sep; make it so
		else
			hosts=$(expand_epname "$raw_token" "$OS_TENANT_NAME" $3) 
		fi

		expiry=$( str2expiry $2 )
		rjprt  $opts -m POST -D "ow_reserve $v6_flag $kv_pairs $1 $expiry $hosts $4 $5" -t "$proto$host/$bandwidth"
		;;

	setdiscount)
		rjprt  $opts -m POST -D "$token setdiscount $2" -t "$proto$host/$bandwidth"
		;;

	setulcap)
		rjprt  $opts -m POST -D "$token setulcap $2 $3" -t "$proto$host/$default"
		;;

	steer*)
		expiry=$( str2expiry $2 )
		rjprt  $opts -m POST -D "steer $kv_pairs $expiry ${3//%t/$raw_token} $4 $5 $6 $7" -t "$proto$host/$steering"
		;;

	verbose)
		case $2 in
			[0-9]*) rjprt  $opts -m POST -D "$token verbose $2 $3" -t "$proto$host/$default";;		# assume tegu way: level subsystem
			*) 		rjprt  $opts -m POST -D "$token verbose $3 $2" -t "$proto$host/$default";;		# assume entered backwards: subsystem level
		esac
		;;

	add-mirror|addmirror)
		shift
		if (( $# < 3 ))
		then
			echo "bad number of positional parms for add-mirror  [FAIL]" >&2
			usage >&2
			exit 1
		fi
		json="{"

		case $1 in 		# handle [start-]end or +sss
			*-*)					# start-end
				json="$json \"start_time\": \"${1%%-*}\", \"end_time\": \"${1##*-}\","
				;;

			+[0-9]*)				# number of seconds after now
				now=$( date +%s )
				json="$json \"start_time\": \"${now}\", \"end_time\": \"$((now $1))\","
				;;

			[0-9]*)					# just a hard end
				now=$( date +%s )
				if (( $1 < now ))
				then
					echo "end time ($1) is not in the future"
					echo "invalid window: expected [start-]end or +sss  [FAIL]"
					usage
					exit 1
				fi

				json="$json \"start_time\": \"${now}\", \"end_time\": \"$1\","
				;;

			*)
				echo "invalid window: expected [start-]end or +sss   [FAIL]"
				usage
				exit 1
				;;
		esac

		json="$json \"output\": \"$3\", \"port\": [ "
		sep=""
		for p in $( echo $2 | tr , ' ' )
		do
			json="$json$sep\"$p\""
			sep=", "
		done
		json="$json ]"
		if (( $# >= 4 ))
		then
			json="$json, \"cookie\": \"$4\""
		fi
		if (( $# >= 5 ))
		then
			json="$json, \"vlan\": \"$5\""
		fi
		json="$json }"
		rjprt $opts -m POST -D "$json" -t "$proto$host/tegu/mirrors/"
		;;

	del-mirror|delmirror)
		shift
		case $# in
			1)
				rjprt $opts -m DELETE -t "$proto$host/tegu/mirrors/$1/" </dev/null
				;;
			2)
				rjprt $opts -m DELETE -t "$proto$host/tegu/mirrors/$1/?cookie=$2" </dev/null
				;;
			*)
				echo "bad number of positional parameters for del-mirror [FAIL]" >&2
				usage >&2
				exit 1
				;;
		esac
		;;

	list-mirrors|listmirror)
		rjprt $opts -m GET -t "$proto$host/tegu/mirrors/"
		;;

	show-mirror|showmirror)
		shift
		case $# in
			1)
				rjprt $opts -m GET -t "$proto$host/tegu/mirrors/$1/"
				;;
			2)
				rjprt $opts -m GET -t "$proto$host/tegu/mirrors/$1/?cookie=$2"
				;;
			*)
				echo "bad number of positional parameters for show-mirror [FAIL]" >&2
				usage >&2
				exit 1
				;;
		esac
		;;

	test)
		shift
		if (( force_endpoints ))
		then
			name2epid $@
		else
			echo "not forced"
		fi

		echo "test: raw_token=($raw_token)"
		echo "test: options: ($opts)"
		if [[ -n $1 ]]
		then
			echo "expanded name: $(expand_epname "$raw_token" "$OS_TENANT_NAME" $1)"
		fi
		;;

	*)
		echo ""
		echo "unrecognised action: $1  [FAIL]" >&2
		echo ""
		usage >&2
		;;
esac
