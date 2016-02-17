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

#	Mnemonic:	ql_bw_fmods
#	Abstract:	Generates all needed flow-mods on an OVS for a bandwidth reservation
#				between src and dest VMs (-s and -d respectively). Src is considered
#				to be the local VM (the VM that is attached to the OVS, and dest is
#				the remote VM (-l and -r could be used, but src and dest are used in
#				the other agent scripts so this is being consistent).  In the case where
#				one of the VMs is a router, and an external IP address is supplied and
#				must be used as the flow-mod match criteria, an indication is needed
#				as to whether the the external IP address is "associated" with the
#				source or destination VM.  This is indicated by the presence of the -S
#				or -D option on the command line.
#
#				Bandwidth reservation flow mods are set up this way:
#					inbound
#						p450 Match:	
#								meta == 0 &&
#								vlan id &&
#								reservation VM0 &&
#								reservation VM1 [&& external IP]
#							 Action:
#								[strip dscp value]
#								set metadata 0x01 (resub 90)
#								resub 0 for openstack fmod application
#
#					outbound
#						p400 Match:
#								meta == 0 &&
#								inbound openflow port &&
#								reservation VM0 &&
#								reservation VM1 [&& external IP]
#							 Action:
#								mark with meta value (resub 90)
#								set dscp value
#								resub 0 to apply openstack fmods
#
#				In the brave new world of endpoints (using uuids rather than mac addresses from Tegu),
#				and to deal with the fact that openstack happily allows duplicate MAC addresses, we need
#				to do the following:
#					1) using the local (source) uuid, suss out the vlan id and the openflow port number
#					   from ovs. 
#					2) match outbound traffic based on the inbound openflow port/mac combo since that
#					   is unique where MAC might not be.
#					3) match inbound traffic based on the mac/vlan combination as that is unique
#					   where mac alone is not.
#
#				Transport protocol (tcp/udp) and ports are pesky things. The reservation may have one
#				associated with either endpoint.  The invocation of this script uses -p and -P to 
#				associate a protocol and port port number with the remote endpoint (-r) and the local
#				endpoint (-s).  For the local endpoint -P is used and -p is used if there is a proto/port
#				associated with the remote endpoint.  
#							
#	Date:		20 March 2015
# 	Author: 	E. Scott Daniels
#
#	Mods:		22 Mar 2015 - Added keep on exit option.
#				27 Mar 2015 - Added ipv6 support.
#				20 Apr 2015 - Accept external IP direction
#				11 May 2015 - Inbound flow-mods must match all types, changed to allow for this.
#				14 May 2015 - To eliminate the use of br-rl and thus the last HTB queue. (flow-mods are
#								now very simple, one in each direction)
#				28 May 2015 - Added match vlan support (-V)
#				18 Jun 2015 - Better handling of -q allowing HTB shutoff to be affected completely by
#								agent scripts (Tegu still thinks it's being set!)
#				09 Oct 2015 - Added ability to xlate a neutron uuid into a mac/vlan/ofport tuple.
#				16 Oct 2015 - Tweaked protocol args on the send_ovs_fmod commands.
#				20 Oct 2015 - Correct bug that was not marking the protocol correctly (was putting on all src
#								or all dest on both inbound and outbound fmods rather than src for one and
#								dest for the other.
#				13 Nov 2015 - Now susses the bridge based on uuid from ovs rather than assuming br-int.
#				14 Jan 2016 - Fixed typos in comments/doc.
#				16 Jan 2016 - Added ability to set rate limiting flow-mods (yet again) if the bridge is
#								listed in the config file on the bandwidth:rate-limit variable.
#				18 Jan 2016 - Added ability to detect and properly handle a source TCP address (e.g.
#								token/project/endpoint/123.45.67.89[:port][{vlan}] can be supplied on the 
#								reservation to Tegu which yields -P {tcp|udp}[:address]:{port|0}. 
#								Deprecated running on remote host with -h option.
# ---------------------------------------------------------------------------------------------------------

trap "cleanup" EXIT

# ensure all tmp files are gone on exit
function cleanup
{
	rm -f /tmp/PID$$.*
}

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	echo "$argv0 v1.1/15125"
	echo "usage: $argv0 [-6] [-d dest-endpt] [-E external-ip] [-h host] [-k] [-n] [-o] [-p|P proto[:addr]:port] [-s src-endpt] [-T dscp] [-t hard-timeout] [-v]"
	echo "usage: $argv0 [-X] 		# delete all"
	echo "" echo "  -6 forces IPv6 address matching to be set"
}

function suss_ovs_data
{
	if [[ ! -s $ovs_data ]]
	then
		ql_suss_ovsd >$ovs_data
	fi
}

# accept either a uuid or mac and returns the the mac, vlan-id and openflow port associated.
# guarenteed to be correct if the uuid (neutron) is passed in, and probably correct when
# given a mac unless there are two devices with the same mac address attached to the OVS
# which seems wrong, but possible.
# Output is echoed to stdout in this order: mac, vlan, port.
function uuid2mac 
{
	#CAUTION: don't write to stdout other than the data from awk
	suss_ovs_data

	if [[ $1 == *":"* ]]		# we assume that a uuid does not have colons
	then
		echo "$1 -1 0"			# no vlan or port if mac is passed in (we assume this is the remote side)
		return
	fi

	#ql_suss_ovsd | grep $1 | awk '{ print $5, $7, $3, $8 }'		#  mac, vlan, port, bridge
	grep "$1" ${ovs_data:-/dev/null} | awk '{ print $5, $7, $3, $8 }'		#  mac, vlan, port, bridge
}

# given a bridge name, find the qosirl0 and qosirl10 port numbers (irl0 is out, irl10 is in)
# port numbers are written on stdout for caller (out in).  qosirl1 is a port on the rate limiting
# bridge.
function get_rlports
{
	#CAUTION: don't write to stdout other than the port from awk

	if [[ -z $1 ]]
	then
		echo "-1"
		return
	fi

	suss_ovs_data

	awk -v bridge="$1" '
		BEGIN { 
			iport = -1 
			oport = -1 
			otarget = bridge "-qosirl0"
			itarget = bridge "-qosirl10"
		}
		/^port: / &&  $8 == bridge  && $4 == otarget { oport = $3; next; }
		/^port: / &&  $8 == bridge  && $4 == itarget { iport = $3; next; }
		END {
			printf( "%d %d\n", oport, iport )		# dump the output and input port numbers
		}
	' $ovs_data
}

# ----------------------------------------------------------------------------------------------------------

typeset -C bandwidth			# must predefine on the off chance that config is nil
config="${TEGU_AGENT_CONFIG:-tegu_agent.cfg}"

bridge="br-int"
ovs_data=/tmp/PID$$.data		# data from suss_ovsd

lmac=""					# local mac 	src outbound, dest inbound
rmac=""					# remote mac	src inbound, dest outbound
queue=""
idscp=""
odscp=""
forreal=""
pri_base=0				# priority is bumpped up a bit for protocol specific f-mods
vp_base=0				# priority added if vlan match supplied (outbound)
one_switch=0			# may need to handle things differently if one switch is involved
koe=0					# keep dscp value as packet 'exits' our environment. Set if global_* traffic type given to tegu
to_value="61"			# value used to check (without option flag)
timout="-t $to_value"	# timeout parm given on command
operation="add"			# -X allows short time durations for deletes
ip_type="-4"			# default to forcing an IP type match for outbound fmods; inbound fmods do NOT use this
ex_local=1				# the external IP is "associated" with the local when 1 (-S) and with the remote when 0 (-D)
set_vlan=1				# -v causes all vlan processing to be ignored

ob_lproto=""			# out/inbound local protocol Set with -P
ib_lproto=""
ob_rproto=""			# out/inbound remote proto set sith -p
ib_rproto=""

ob_lproto=""            # out/inbound local protocol Set with -P
ib_lproto=""
ob_rproto=""            # out/inbound remote proto set sith -p
ib_rproto=""

typeset -C bandwidth											# must ensure this is set to handle missing config file
ql_parse_config -f $config >/tmp/PID$$.cfg && . /tmp/PID$$.cfg		# xlate config file into variables and source them
bandwidth.cookie=${bandwidth.cookie:-0xb0ff}					# default if not in config or config missing

while [[ $1 == -* ]]
do
	case $1 in
		-6)		ip_type="-6";;								# force ip6 option to be given to send_ovs_fmod (outbound only).
		-d)		uuid2mac "$2" | read rmac dvlan dofport dbridge junk; 	# get the mac, vlan, bridge and openflow port from ovs based on neutron uuid
				duuid=$2												# could be needed for rate limiting if they reversed -s and -d
				shift
				;;

		-D)		ex_local=0;;								# external IP is "associated" with the rmac (-d) address
		-E)		exip="$2"; shift;;

		-h)		echo "ERROR: running on a remote host is deprecated; -h is not supported  [FAIL]"
				exit 1
				;;

		-k)		koe=1;;

		-n)		forreal="-n"
				trace_on="set -x"
				trace_off="set +x"
				;;

		-o)		one_switch=1;;

		-P)		pri_base=5;												# source proto:[addr:]port priority must increase to match over more generic f-mods
				ql_split_pap "$2" | read proto addr port ip_type junk	# expect {tcp|udp}:[address:]port ip-type-flag
				if [[ $addr != "none" ]]
				then
					ob_netaddr="-S $addr"					# transport protocol address
					ib_netaddr="-D $addr"
				fi
				ob_rproto="-P $proto:${port:-0}"			# proto matches -d endpoint (remote), proto for outbound dest(P), inbound src(p)
				ib_rproto="-p $proto:${port:-0}" 
				shift
				;;		

		-p)		pri_base=5	 								# dest proto:port priority must increase to match over more generic f-mods
				split_pap "$2" | read proto addr port ip_type junk	# expect {tcp|udp}:[address:]port
				if [[ $addr != "none" ]]
				then
					ib_netaddr="-S $addr"					# transport protocol address
					ob_netaddr="-D $addr"
				fi
				ob_lproto="-p $proto:${port:-0}" 			# proto matches -s (local) endpoint: proto for outbound is src(p), inbound dest(P)
				ib_lproto="-P $proto:${port:-0}" 			# proto matches -s (local) endpoint: proto for outbound is src(p), inbound dest(P)
				shift
				;;		

		-q)		queue="-q $2"; shift;;									# soon to change to meter
		-s)		uuid2mac "$2" | read lmac svlan sofport sbridge junk; 	# get the mac, vlan and openflow port from ovs based on neutron uuid
				luuid=$2												# we need this to set -R port if rate limiting
				shift
				;;

		-S)		ex_local=1;;								# external IP is "associaetd" with the lmac (-s) address.
		-t)		to_value=$2; timeout="-t $2"; shift;;
		-T)		odscp="-T $2"; shift;;
		-v)		set_vlan=0;;							# ignored -- maintained for backwards compat
		-V)		vp_base=5; match_vlan="-v $2"; shift;;	# vlan id given on resrvation for match (applies only to outbound)
		-X)		operation="del";;

		-\?)	usage
				exit 0
				;;

		*)	echo "unrecognised option: $1"
			usage
			exit 1
			;;
	esac

	shift
done


match_port=""
match_vlan=""
if (( sofport > 0 ))				# source should be local and what we have, so use it first
then
	vp_base=5
	match_port="-i $sofport"		# for outbound fmod
	if (( svlan >= 0 ))
	then
		match_vlan="-v $svlan"			# for inbound fmod
		set_vlan="-v $svlan"
	fi
	logit "openflow vlan/port ($svlan, $sofport) captured from source uuid"
else
	if (( dofport > 0 ))			# if caller got them backwards
	then
		luuid=$duuid					# switch in case we're rate limiting
		vp_base=5
		match_port="-i $dofport"		# for outbound fmod
		if (( dvlan >= 0 ))
		then
			match_vlan="-v $dvlan"			# for inbound fmod
			set_vlan="-v $dvlan"
		fi
		logit "openflow vlan/port ($dvlan, $dofport) captured from dest uuid"
	else
		logit "openflow port and vlan information not found"
	fi
fi

if [[ -n sbridge ]]
then
	logit "setting bridge based on sbridge: $sbridge"
	bridge=$sbridge
fi

# CAUTION:  this is confusing, so be careful (see notes in flower box at top)
if [[ -n $exip ]]						# need to set up matching for external
then
	if [[ $exip == "["*"]" ]]
	then
		ip_type="-6"					# force ip type to v6
		exip="${exip#*\[}"
		exip="${exip%\]*}"
	fi
	if (( ex_local ))					# the lmac is associated with the external IP address
	then
		oexip="-S $exip"				# for outbound, the external ip is the src
		iexip="-D $exip"				# for inbound the external ip is the dest
	else
		oexip="-D $exip"				# rmac is associated with external IP, thus outbound external IP is dest
		iexip="-S $exip"				# and inbound the external is source.
	fi
else
	oexip=""
	iexip=""
fi

if [[ -z $lmac || -z $rmac ]]
then
	logit "must have source and dest mac addresses in order to generate flow-mods   [FAIL]"
	rm -f /tmp/PID$$.*
	exit 1
fi

if [[ -z $bridge ]]
then
	logit "flow_mods not set: unable to determine bridge"
	exit 1
fi

if (( koe ))			# true == keep dscp marking as packet 'exits' our control area
then
	idscp=""			# don't reset the dscp value on inbound (exiting) traffic
else
	idscp="-T 0"		# keep off, force marking to 0
fi

if (( ! one_switch )) && [[ -n ${bandwidth.rate_limit} ]]  &&  [[ " ${bandwidth.rate_limit} "  ==  *" $bridge "* ]]				
then 			# if this bridge is flagged for rate limiting, push all traffic through the rate limiting extension bridge
	get_rlports $bridge | read rl_oport rl_iport		# get oubound and inbound ports

	if (( $rl_iport <= 0 ))		# TESTING
	then
		rl_iport=$rl_oport
		echo ">>>> fix me now" >&2
		exit 1
	fi
	#
	# outbound
	#	p900: source is endpoint & from rl bridge: set in-port to the endpoint's port, strip vlan id,  resub (,0) for normal
	#	p400+base: source is endpoint optionally w/IP and port & traffic is NOT from the rl bridge: push traffic on the rl bridge with queue
	#
	# inbound
	#	dest is endpoint set meta data and resubmit to prevent p10 flow-mod from marking down
	#		(this flow-mod is not added if koe flag is not set)
	#
	# flow-mods must go in in this order, and we must abort if there is a failure. 

	to_rl=" -o $rl_oport"					# to/from parms for fmod
	fr_rl=" -i $rl_iport"
	rl_port=JUNK_FIX_THE_CODE

	# outbound - match traffic from rate limiting bridge
	# manual prototype: sudo ovs-ofctl -O OpenFlow10 add-flow br-int 'hard_timeout=210,cookie=0xbaff,metadata=0/0x7,dl_src=fa:de:ad:f8:d0:0e,in_port=57,priority=900,action=strip_vlan,set_field:9->in_port,set_field:0x01->metadata,NORMAL'
	if ! send_ovs_fmod $forreal -p 900 $timeout --match -m 0/0x07 -i $rl_iport -s $lmac  -d $rmac $ob_lproto $ob_rproto $ob_netaddr --action -M 0x01 -V -i $luuid -R ,0 -N  $operation ${bandwidth.cookie} $bridge
	then
		logit "could not set flow-mod to route reservation traffic from rate limiting bridge	[FAIL]"
		exit 1
	fi

	#TODO -- add queue
	# outbound: force the reservation traffic to the rate limiting bridge after setting the vlan (vlan always set to 1 since it's stripped on the way out anyway)
    if ! send_ovs_fmod $forreal -p $(( pri_base + 400 )) $timeout --match -s $lmac -d $rmac -m 0/0x07 $ip_type $iexip  $ob_lproto $ob_rproto $ob_netaddr --action  -v 1 -o $rl_oport  $operation ${bandwidth.cookie} $bridge
	then
		logit "could not set rate limiting p 400 (out to br-rl) flowmod	[FAIL]"
		exit 1
	fi

	# inbound -- if outbound fmods were successful, and koe flag, prevent DSCP umarking
	if (( koe ))
	then
    	if ! send_ovs_fmod $forreal -p $(( pri_base + 300 )) $timeout --match -d $lmac -s $rmac -m 0/0x07 $ip_type $iexip  $ib_lproto $ib_rproto $ib_netaddr --action  -M 0x01 $operation ${bandwidth.cookie} $bridge	# prevent p10 rule from marking down
		then
			logit "unable to create inbound flow-mod to prevent DSCP markings from being removed (koe)	[FAIL]"
			exit 1
		fi
	fi
else
	# CAUTION:	action options to send_ovs_fmods are probably order dependent, so be careful.
	# DANGER:	NEVER set queues here, only if bridge is rate limiting and above code will handle
	if (( ! one_switch ))
	then
		# inbound -- only if they are not on the same switch
		$trace_on
		send_ovs_fmod $forreal $timeout -p $(( 450 + pri_base )) --match $match_vlan $ip_type -m 0x0/0x7 $iexip -d $lmac -s $rmac $ib_lproto $ib_rproto $ib_netaddr --action $idscp -M 0x01 -R ,0 -N $operation ${bandwidth.cookie} $bridge
		rc=$?
		$trace_off
	else
		if (( ! koe ))		# one switch and keep is off, no need to set dscp on the outbound fmod
		then
			odscp=""
		fi
	fi

	#outbound
	if [[ -n $match_port ]]			# if we have an input port, we can drop the source mac match (local mac)
	then							
		ob_smac=""
	else
		ob_smac="-s $lmac"
	fi
	
	$trace_on
	send_ovs_fmod $forreal $timeout -p $(( 400 + vp_base + pri_base )) --match  $match_port $ip_type -m 0x0/0x7 $oexip $ob_smac -d $rmac $ob_lproto $ob_rproto $ob_netaddr --action $odscp -M 0x01  -R ,0 -N $operation ${bandwidth.cookie} $bridge
	(( rc = rc + $? ))
	$trace_off
fi

cleanup $(( rc > 0 ))			# exit 1 if any number of failures earlier
