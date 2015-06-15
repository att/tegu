#!/usr/bin/env ksh
# vi: ts=4 sw=4:

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
#								reservation VM0 &&
#								external-IP [&& proto:port]
#							 Action:
#								mark with meta value (-M)
#								set dscp value
#								resub 0 to apply openstack fmods
#
#				We no longer need to set VLAN on outbound nor do we need to strip VLAN on inbound, so
#				vlan options are currently ignored (supported to be compatable with old/unchanged
#				agents).  Same with queues. We aren't queuing at the moment so the queue options are
#				ignored. In future, there will (should) be a concept of flow-limits (meters maybe)
#				which will be passed in as queue numbers, so the -q option needs to be kept and should
#				be expected and used when the underlying network compoents can support it.
#							
#	Date:		15 June 2015
# 	Author: 	E. Scott Daniels
#
#	Mods:
# ---------------------------------------------------------------------------------------------------------

function logit
{
	echo "$(date "+%s %Y/%m/%d %H:%M:%S") $argv0: $@" >&2
}

function usage
{
	echo "$argv0 v1.0/16155"
	echo "usage: $argv0 [-6] [-d dst-mac] [-E external-ip] [-h host] [-n] [-p|P proto:port] [-s src-mac] [-T dscp] [-t hard-timeout]"
	echo "usage: $argv0 [-X] # delete all"
	echo ""
	echo "  -6 forces IPv6 address matching to be set"
}


# ----------------------------------------------------------------------------------------------------------

cookie="0xf00d"			# static for now, but might want to make them user controlled, so set them up here
bridge="br-int"
mt_base=90				# meta table base 90 sets 0x01, 91 sets 0x02, 94 sets 0x04...

smac=""					# src mac address (local to this OVS)
dmac=""					# dest mac (remote if not x-project)
exip=""					# external (dest) IP address (if x-project, or dest proto supplied)
queue=""
idscp=""
odscp=""
host=""
forreal=""
pri_base=0				# priority is bumpped up a bit for protocol specific f-mods
queue=""
to_value="61"			# value used to check (without option flag)
timout="-t $to_value"	# timeout parm given on command
operation="add"			# -X sets delete action
ip_type="-4"			# default to forcing an IP type match for outbound fmods; inbound fmods do NOT use this

while [[ $1 == -* ]]
do
	case $1 in
		-6)		ip_type="-6";;							# force ip6 option to be given to send_ovs_fmod
		-d)		dmac="-d $2"; shift;;					# dest (remote) mac address (could be missing)
		-E)		exip="$2"; shift;;
		-h)		host="-h $2"; shift;;
		-n)		forreal="-n";;
		-p)		pri_base=5; sproto="-p $2"; shift;;		# source proto:port priority must increase to match over more generic f-mods
		-P)		pri_base=5; dproto="-P $2"; shift;;		# dest proto:port priority must increase to match over more generic f-mods
		-q)		queue="-q $2"; shift;;					# ignored until HTB replacedment is found
		-s)		smac="$2"; shift;;						# source (local) mac address
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

if [[ -n $exip ]]
then
	exip="-D $exip"
fi

if [[ -n $sproto && -z sip ]]			# must have a source IP if source proto is supplied
then
	logit "source IP address required when source prototype is supplied   [FAIL]"
	exit 1
fi

if [[ -n $dproto && -z $exip ]]
then
	logit "external (-E) ip address required when destionation prototype (-P) given    [FAIL]"
	exit 1
fi

if [[ -z $dmac && -z $exip ]]		# fail if both missing
then
	logit "must have either destination mac address or external IP address to generate oneway flow-mods; both missing   [FAIL]"
	exit 1
fi


# CAUTION: action options to send_ovs_fmods are probably order dependent, so be careful.
set -x
send_ovs_fmod $forreal $host $timeout -p $(( 400 + pri_base )) --match  $match_vlan $ip_type -m 0x0/0x7 $sip $exip -s $smac $dmac $dproto $sproto --action $odscp -M 0x01  -R ,0 -N $operation $cookie $bridge
rc=$(( rc + $? ))
set +x

rm -f /tmp/PID$$.*
if (( rc ))
then
	exit 1
fi

exit 0
