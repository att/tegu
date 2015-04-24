#!/usr/bin/env ksh

#	Mnemonic:	ql_fiilter_rtr
#	Abstract:	Run various ip commands to get a list of router/gw mac addresses that are currently
# 				active on the node.  Using the list, and the output from ovs_sp2uuid (stdin) 
#				generate an augmented set of ovs_sp2uuid output.  The filter applied allows all 
#				non-router elements to pass, and blocks router elements if there is no corresponding 
#				mac address listed by the series of ip commands.
#
# 				Running through the name spaces seems to be expensive, based on a trial on a loaded L3,
#				so we'll cache the list and regenerate it only if we think it is out of date. 
#			
#				DANGER:  This can be _extremely_ expensive (in terms of wall clock execution time)
#						and thus any code that calls it, and is expected to return a result quickly
#						might not.  The run time is proportional to the number of routers that are
#						existing on the host (readl longer for an L3).
#
#	Author: 	E. Scott Daniels
#	Date:		14 April 2015
# --------------------------------------------------------------------------------------------------


age=300								# reload only every 5 minutes by default
while [[ $1 == -* ]]
do
	case $1 in 
		-a)		age=$2; shift;;
		-f)		age=0;;				# force a reload of data regardless of how old the cache is

		*)	echo "unrecognised option $1"
			echo "usage: $0 [-a age|-f] [data-file]"
			echo "data file is the output from ovs_sp2uuid; if omitted the output is assumed to be on stdin"
			exit 1
			;;
	esac

	shift
done

user=${USER:-$LOGNAME}					# one of these should be set; prevent issues if someone runs manually
cache=/tmp/ql_rmac_${user}.cache
if [[ -f /tmp/ql_filter_rtr.v ]]		# preliminary testing; delete next go round of changes
then
	verbose=1
fi

need=0
if [[ ! -s $cache  ]]					# generate if empty or missing
then
	need=1
else
	ts=$( stat -c %Y $cache )			# last mod time of the cache
	now=$( date +%s )
	if (( now - ts > age ))
	then
		need=1
	fi
fi

if (( need ))
then
	echo "snarfing netns data"
	ip netns | grep "qrouter-" | while read r 			# suss out the list of router name spaces
	do 
		if (( verbose ))
		then
			echo "suss from $r   [OK]" >&2
		fi
		sudo ip netns exec $r ifconfig | grep HWaddr	# query each name space, get all associated mac addresses
	done >/tmp/PID$$.cache								# prevent accidents if multiple copies running

	mv /tmp/PID$$.cache $cache							# if multiple copies running, this is atomic
fi

(
	awk '{ print $NF }' $cache | sort -u | sed 's/^/mac: /'		# mac addresses MUST be first
	cat $1														# useful use of cat -- read stdin or from file if given
) | awk '
	/^mac: / {											# collect mac addresses
			mac[$2] = 1;
			next;
	}

	/^port.*qg-/ {										# allow to pass only if a mac address was seen
		if( mac[$5] == 1 )
			print;
		next;
	}

	/^port.*qr-/ {
		if( mac[$5] == 1 )
			print;
		next;
	}

	{ print; next; }
	'

exit 0
