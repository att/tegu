#!/usr/bin/env ksh
# vi: ts=4 sw=4:
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
# ----------------------------------------------------------------------------------------

function usage {
	cat <<endKat

	usage: $argv0 [-d] [-h tegu-host[:port] [-j] [-K] [-k key=value] [-r rname] [-s] [-t token|-T] command parms

	  -d causes json output from tegu to be formatted in a dotted hierarch style
	  -f force prompting for user and password if -T is used even if a user name or password is
	     currrently set in the environment.
	  -h is needed when tegu is running on a different host than is being used to run tegu_req
	     and/or when tegu is listening on a port that isn't the default
	  -j causes raw json to be spilled to the standard out device
	  -k k=v Supplies a key/value pair that is necessary on some requests. Multiple -k options
	     can be supplied when needed.
	  -K Use keystone command line interface, rather than direct API, to generate a token
	     (ignored unless -T is used)
	  -r allows a 'root' name to be supplied for the json output when humanised
	  -s enables secure TLS (https://) protocol for requests to Tegu.
	  -t allows a keystone token to be supplied for privileged commands; -T causes a token to
	     be generated using the various OS_ environment variables. If a needed variable is
	     not in the environment, then a prompt will be issued. When either -t or -T is given
	     a %t can be used on the commandline in place of the token and the token will
	     substituted. For example: %t/cloudqos/daniels8  would substitute the generated
	     token into the host name specification.

	commands and parms are one of the following:
	  $argv0 reserve [bandwidth_in,]bandwidth_out [start-]expiry token/project/host1,token/project/host2 cookie [dscp]
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

	  If only bandwidth_out is supplied, then that amount of bandwidth is reserved
	  in each direction. Otherwise, the bandwidth out value is used to reserve
	  bandwidth from host1 (out) to host2 and the bandwidth in is used to reserve
	  bandwidth from host2 (in) to host1. Both values may be specified with trailing
	  G/M/K suffixes (e.g. 10M,20M).

	  The dscp value is the desired value that should be left tagging the data as it
	  reaches the egress point.  This allows applications to have their data tagged
	  in cases when the application does not, or cannot, tag it's own data.

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
	typeset token_value=""
	typeset xOS_PASSWORD=""
	typeset xOS_USERNAME=""
	typeset xOS_TENANT_NAME=""

	trap 'stty echo; exit 2' 1 2 3 15
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
	trap - 1 2 3 15

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

argv0="${0##*/}"
port=29444
host=localhost:$port
opts=""
root=""
proto="http"
prompt4token=0
force=0
use_keystone=0

bandwidth="bandwidth"		# http api collections
steering="api"				# eventually this should become steering
default="api"

while [[ $1 == -* ]]
do
	case $1 in
		-d)		opts+=" -d";;
		-f)		force=1;;
		-F)		bandwidth="api"; steering="api";;		# force collection to old single set style
		-h) 	host=$2; shift;;
		-j)		opts+=" -j";;
		-k)		kv_pairs+="$2 "; shift;;
		-K)		use_keystone=1;;
		-r)		root="$2"; shift;;
		-s)		proto="https";;
		-t)		raw_token="$2"; token=$"auth=$2"; shift;;
		-T)		prompt4token=1;;
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

opts+=" -r ${root:-$1}"

if (( force > 0 ))							# force username and password prompts; other OS vars default if set
then
	OS_USERNAME=""
	OS_PASSWORD=""
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
		rjprt  $opts -m POST -t "$proto://$host/tegu/$default" -D "$token ping"
		;;

	listq*|qdump|dumpqueue*)
		rjprt  $opts -m POST -t "$proto://$host/tegu/$bandwidth" -D "$token qdump"
		;;

	listr*)
		rjprt  $opts -m POST -t "$proto://$host/tegu/$default" -D "$token listres $kv_pairs"
		;;

	listh*)						# list hosts
		rjprt  $opts -m POST -t "$proto://$host/tegu/$default" -D "$token listhosts $kv_pairs"
		;;

	listul*)						# list user link caps
		rjprt  $opts -m POST -t "$proto://$host/tegu/$bandwidth" -D "$token listulcaps"
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

		rjprt  $opts -m POST -t "$proto://$host/tegu/$default" </tmp/PID$$.data
		rm -f /tmp/PID$$.data
		;;

	graph)
		rjprt  $opts -m POST -D "$token graph $kv_pairs" -t "$proto://$host/tegu/$default"
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

		rjprt $opts -m DELETE -D "reservation $1 $2" -t "$proto://$host/tegu/$bandwidth"
		;;

	pause)
		rjprt $opts -m POST -D "$token pause" -t "$proto://$host/tegu/$default"
		;;

	refresh)
		rjprt  $opts -m POST -D "$token refresh $2" -t "$proto://$host/tegu/$default"
		;;

	resume)
		rjprt $opts -m POST -D "$token resume" -t "$proto://$host/tegu/$default"
		;;

	reserve)
		shift
			#teg command is: reserve <bandwidth>[K|M|G] [<start>-]<end>  <host1-host2> [cookie [dscp]]
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
		if [[ $3 == *"-any" ]] || [[ $3 == *",any" ]]
		then
			echo "second host in the pair must NOT be 'any'   [FAIL]" >&2
			exit 1
		fi
		if [[ -n $5 ]]
		then
			if (( $5 < 0 || $5 > 64 ))
			then
				echo "dscp value ($5) must be between 0 and 64  [FAIL]" >&2
				exit 1
			fi
		fi
		rjprt  $opts -m POST -D "reserve $kv_pairs $1 $expiry ${3//%t/$raw_token} $4 $5" -t "$proto://$host/tegu/$bandwidth"
		;;

	owres*|ow_res*)
		shift
			#teg command is: owreserve <bandwidth>[K|M|G] [<start>-]<end>  <host1-host2> [cookie [dscp]]
		if (( $# < 4 ))
		then
			echo "bad number of positional parms for owreserve  [FAIL]" >&2
			usage >&2
			exit 1
		fi
		expiry=$( str2expiry $2 )
		rjprt  $opts -m POST -D "ow_reserve $kv_pairs $1 $expiry ${3//%t/$raw_token} $4 $5" -t "$proto://$host/tegu/$bandwidth"
		;;

	setdiscount)
		rjprt  $opts -m POST -D "$token setdiscount $2" -t "$proto://$host/tegu/$bandwidth"
		;;

	setulcap)
		rjprt  $opts -m POST -D "$token setulcap $2 $3" -t "$proto://$host/tegu/$default"
		;;

	steer*)
		expiry=$( str2expiry $2 )
		rjprt  $opts -m POST -D "steer $kv_pairs $expiry ${3//%t/$raw_token} $4 $5 $6 $7" -t "$proto://$host/tegu/$steering"
		;;

	verbose)
		case $2 in
			[0-9]*) rjprt  $opts -m POST -D "$token verbose $2 $3" -t "$proto://$host/tegu/$default";;		# assume tegu way: level subsystem
			*) 		rjprt  $opts -m POST -D "$token verbose $3 $2" -t "$proto://$host/tegu/$default";;		# assume entered backwards: subsystem level
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
		rjprt $opts -m POST -D "$json" -t "$proto://$host/tegu/mirrors/"
		;;

	del-mirror|delmirror)
		shift
		case $# in
			1)
				rjprt $opts -m DELETE -t "$proto://$host/tegu/mirrors/$1/" </dev/null
				;;
			2)
				rjprt $opts -m DELETE -t "$proto://$host/tegu/mirrors/$1/?cookie=$2" </dev/null
				;;
			*)
				echo "bad number of positional parameters for del-mirror [FAIL]" >&2
				usage >&2
				exit 1
				;;
		esac
		;;

	list-mirrors|listmirror)
		rjprt $opts -m GET -t "$proto://$host/tegu/mirrors/"
		;;

	show-mirror|showmirror)
		shift
		case $# in
			1)
				rjprt $opts -m GET -t "$proto://$host/tegu/mirrors/$1/"
				;;
			2)
				rjprt $opts -m GET -t "$proto://$host/tegu/mirrors/$1/?cookie=$2"
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
		echo "test: raw_token=($raw_token)"
		echo "test: options: ($opts)"
		;;

	*)
		echo ""
		echo "unrecognised action: $1  [FAIL]" >&2
		echo ""
		usage >&2
		;;
esac
