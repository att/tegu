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

#	Mnemonic:	ql_bwow_fmods
#	Abstract:	Generates all needed flow-mods on an OVS for a oneway bandwidth reservation.
#				One way reservations mark and potentially rate limit traffic on the ingress
#				OVS only.  There is no attempt to set any flow-mods for inbound traffic as
#				we do NOT expect that the traffic has been marked by us on the way "in".
#				A oneway reservation is generally implemented when the other endpint is
#				external (cross project, or on the other side of the NAT box), and the router
#				is not a neutron router (i.e. not under OVS).
#
#				Bandwidth reservation flow mods are set up this way:
#					inbound (none)
#
#					outbound
#						p400 Match:
#								meta == 0 &&
#								openflow inport &&
#								reservation VM0 &&
#								external-IP [&& proto:port]
#							 Action:
#								mark with meta value (-M)
#								set dscp value
#								resub 0 to apply openstack fmods
#
# 				In the world of endpoints, and Tegu sending a uuid as the source endpoint, we
#				must map that uuid (neutron) to a local openflow port as the port/mac is the 
#				only way to guarentee a positive match on the traffic because macs can be duplicated.
#							
#	Date:		15 June 2015
# 	Author: 	E. Scott Daniels
#
#	Mods:		17 Jun 2015 - Corrected handling of queue value when 0.
#				09 Oct 2015 - Added ability to accept a neutron uuid for translation to mac/port.
#				16 Nov 2015 - Now susses the bridge based on uuid from ovs rather than assuming br-int.
#				15 Feb 2015 - Add rate limiting support, deprecate -h option.
# ---------------------------------------------------------------------------------------------------------

# via broker on qos102: PATH=/tmp/daniels_b:$PATH ql_bwow_fmods -s 9458f3be-0a84-4b29-8e33-073ceab8d6e4 -d fa:16:3e:ed:cc:e5 -p udp: -q 2 -t 59 -T 184 -V 2 

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
	echo "$argv0 v1.0/16155"
	echo "usage: $argv0 [-6] [-d dst-uuid] [-E external-ip] [-n] [-p|P proto:port] [-s src-uuid] [-T dscp] [-t hard-timeout]"
	echo "usage: $argv0 [-X] # delete all"
	echo ""
	echo "  -6 forces IPv6 address matching to be set"
	echo "  source and dest uuids are neutron IDs which are looked up in OVS and mapped to mac/vlan/port"
}

# fetch the ovs data once per execution.
function suss_ovs_data
{
	if [[ -s $ovs_data ]]
	then
		return
	fi

	ql_suss_ovsd >$ovs_data
}

# accept either a uuid or mac and returns the the mac, vlan-id and openflow port associated.
# guarenteed to be correct if the uuid (neutron) is passed in, and probably correct when
# given a mac unless there are two devices with the same mac address attached to the OVS
# which seems wrong, but possible.
# Output is echoed to stdout in mac, vlan, port order.
function uuid2mac 
{
	if [[ $1 == *":"* ]]		# we assume that a uuid does not have colons
	then
		echo "$1 -1 -1"			# no vlan if mac is passed in
		return
	fi

	suss_ovs_data               # ensure data pulled from ovs
	grep $1 $ovs_data | awk '{ print $5, $7, $3, $8 }'      #  mac, vlan, port, bridge
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
config="${TEGU_AGENT_CONFIG:-tegu_agent.cfg}"
ovs_data=/tmp/PID$$.ovsd		# output from ovs_susd

bridge="br-int"

smac=""					# src mac address (local to this OVS)
dmac=""					# dest mac (remote if not x-project)
exip=""					# external (dest) IP address (if x-project, or dest proto supplied)
queue=""
idscp=""
odscp=""
forreal=""
pri_base=0				# priority is bumpped up a bit for protocol specific f-mods
queue="0"
to_value="61"			# value used to check (without option flag)
timout="-t $to_value"	# timeout parm given on command
operation="add"			# -X sets delete action
ip_type="-4"			# default to forcing an IP type match for outbound fmods; inbound fmods do NOT use this

typeset -C bandwidth												# must ensure this is set to handle missing config file
bandwidth.cookie="0xf00d"											# default, can be overridden in the agent config
ql_parse_config -f $config >/tmp/PID$$.cfg && . /tmp/PID$$.cfg		# override script defaults with config; command line trumps config


while [[ $1 == -* ]]
do
	case $1 in
		-6)		ip_type="-6";;							# force ip6 option to be given to send_ovs_fmod
		-d)		dmac="-d $2"; shift;;					# dest (remote) mac address (could be missing)
		-E)		exip="$2"; shift;;
		-h)		echo "-h is deprecated"; exit 1;;
		-n)		forreal="-n";;
		-p)		pri_base=5; sproto="-p $2"; shift;;		# source proto:port priority must increase to match over more generic f-mods
		-P)		pri_base=5; dproto="-P $2"; shift;;		# dest proto:port priority must increase to match over more generic f-mods
		-q)		queue="$2"; shift;;						# ignored until HTB replacedment is found
		-s)		uuid2mac "$2" | read smac svlan sofport sbridge; shift
				suuid=$2
				;;
		-S)		sip="-S $2"; shift;;					# local IP needed if local port (-p) is given
		-t)		to_value=$2; timeout="-t $2"; shift;;
		-T)		odscp="-T $2"; shift;;
		-V)		match_vlan="-v $2"; shift;;
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

if [[ -z $smac ]]
then
	logit "must have source mac address in order to generate oneway flow-mods   [FAIL]"
	exit 1
fi

match_port=""
if (( sofport > 0 ))			# must specifically match a port as mac may not be unique
then
	match_port="-i $sofport"
	logit "source uuid was matched and generated a port: $sofport"
fi

if [[ -n $exip ]]
then
	if [[ $exip == "["*"]" ]]
	then
		ip_type="-6"					# force ip type to v6
		exip="${exip#*\[}"
		exip="${exip%\]*}"
	fi
fi

if [[ -n $sbridge ]]
then
	bridge=$sbridge						# use what was sussed from ovs if we can
fi

open_dest=0
if [[ $exip == "any" ]]					# allow destination to be any
then
	exip=""
	open_dest=1
else
	if [[ -n $exip ]]
	then
		exip="-D $exip"
	fi
fi

if (( ! open_dest ))					# if not an open destination (!//any not supplied as second host)
then
	if [[ -z $dmac && -z $exip ]]		# fail if both missing
	then
		logit "must have either destination mac address or external IP address to generate oneway flow-mods; both missing   [FAIL]"
		exit 1
	fi
fi


# DANGER:  we do NOT support trunking VMs with rate limiting because we must fuss with the VLAN id as we push it on and off of the rl bridge
# CAUTION: action options on send_ovs_fmods commands are often order dependent, so be careful.
if [[ -n ${bandwidth.rate_limit} ]]  &&  [[ " ${bandwidth.rate_limit} "  ==  *" $bridge "* ]]				
then 													# if this bridge is flagged for rate limiting, push all traffic through the rate limiting extension bridge
	queue="-q $queue"
	get_rlports $bridge | read rl_oport rl_iport		# get oubound and inbound ports to/from the rate limiting bridge

	if (( rl_oport <= 0  || rl_iport <= 0 ))			# because we test for this, we can invoke set_ovs_fmods with -I option to force it not to try
	then
		logit "could not find rate limit bridge ports for bridge $bridge oport=$rl_oport  iport=$rl_iport	[FAIL]"
		grep $bridge $ovs_data >&2
		exit 1
	fi

	if [[ -z $sofport ]]								# we must have the port in order to do rate limiting so bail if it was not found
	then
		logit "could not determine openflow port for uuid 	[FAIL]"
		exit 1
	fi

	#	p900: source is endpoint & from rl bridge: set in-port to the endpoint's port, strip vlan id,  resub (,0) for normal
	#	p400+base: source is endpoint optionally w/IP and port & traffic is NOT from the rl bridge: push traffic on the rl bridge with queue

	# match traffic from rate limiting bridge: set port to that of the endpoint on the normal bride, strip vlan and then resub
	if ! send_ovs_fmod $forreal -I -p 900 $timeout --match -m 0/0x07 -i $rl_iport -s $smac $dmac $sproto $dproto $exip --action -M 0x01 -V -i $sofport -R ,0 -N  $operation ${bandwidth.cookie} $bridge
	then
		logit "could not set flow-mod to route reservation traffic from rate limiting bridge ($bridge)	[FAIL]"
		exit 1
	fi

	#TODO -- add queue
	# outbound: force the reservation traffic to the rate limiting bridge after setting the vlan
	# we force a dummy vlan since 1) we don't care and 2) we strip it when it comes off of x-rl so it doesn't matter
    if ! send_ovs_fmod $forreal -I -p $(( pri_base + 400 )) $timeout --match -s $smac $dmac -m 0/0x07 $ip_type $iexip  $sproto $dproto $exip --action  $queue -v 1 -o $rl_oport  $operation ${bandwidth.cookie} $bridge
	then
		logit "could not set rate limiting p 400 flow-mod (out to $bridge-rl)	[FAIL]"
		exit 1
	fi
else						# ---- not rate limiting -----
	if (( queue > 0 ))
	then
		echo "-q $queue was ignored: no htb queuing allowed  [OK]"
	fi

	if [[ -n $match_port ]]				# if we were able to determine a port, then we need to drop the src mac address as it's redundant and we cannot support trunking VM with it
	then
		set -x
		send_ovs_fmod $forreal -I $timeout -p $(( 400 + pri_base )) --match $match_vlan $match_port $ip_type -m 0x0/0x7 $sip $exip $dmac $dproto $sproto --action $odscp -M 0x01  -R ,0 -N $operation ${bandwidth.cookie} $bridge
		(( rc =  rc + $? ))
		set +x
	else
		if [[ -n $match_vlan ]]			# if no src port _and_ hard vlan id is set (implying a trunking VM), and we don't have an inbound port, then error
		then
			echo "input port unknown: cannot generate oneway flow-mod on a trunking VM port unless we can determine the input port.    [FAIL]"
		else
			set -x
			send_ovs_fmod $forreal -I $timeout -p $(( 400 + pri_base )) --match $match_port $ip_type -m 0x0/0x7 $sip $exip -s $smac $dmac $dproto $sproto --action $odscp -M 0x01  -R ,0 -N $operation ${bandwidth.cookie} $bridge
			(( rc =  rc + $? ))
			set +x
		fi
	fi
fi

if (( rc ))
then
	exit 1
fi

exit 0
