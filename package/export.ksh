#!/usr/bin/env ksh
#	Mnemonic:	export.ksh
#	Abstract:	reads a list of files and creates the directory structure which can be 
#				tarred and given to a package builder for conversion/inclusion into a
#				package (.deb) file. 
#	Date: 		May 2014
#	Author:		E. Scott Daniels
#
#	Mods:		30 Jan 2015 - Added support for python
#				01 Feb 2015 - Ensure that binaries are chmod'd correctly.
#				28 Apr 2015 - Added auto build of certain binaries.
# -------------------------------------------------------------------------------------------

function usage
{
	echo "$argv0 [-b] [-d export-dir] package-name version"
	echo "  -b turns off the auto build before export"
}

function verbose
{
	if (( chatty ))
	then
		echo "$@"
	fi
}

function build_it
{
	(
		set -e
		cd ../main
		echo "building tegu and agent"
		go build tegu.go
		go build tegu_agent.go

		./tegu -?|grep "^tegu"
		./tegu_agent -?| grep "^tegu"
	)

	if (( $? != 0 ))
	then
		echo "abort: build failed"
		exit 1
	fi
}

# -------------------------------------------------------------------------------------------

ex_root=/tmp/${LOGNAME:=$USER}/export
argv0="${0##*/}"
dir=""
compress_options=""
chatty=0
rebuild=0			# -r sets to mark as a rebuild of a previous package so that last ver is not updated
build=1				# force a build of binaries before exporting, -b turns off

while [[ $1 == -* ]]
do
	case $1 in
		-b)		build=0;;
		-c)		compress_options=$2; shift;;
		-d)		dir=$2; shift;;
		-r)		rebuild=1;;
		-v)		chatty=1;;
		-\?)	usage; exit 0;;
		*)		echo "unrecognised parameter: $1"
				usage
				exit 1
				;;
	esac

	shift
done

if [[ -z $1 ]]
then
	echo "missing package name as first parameter (e.g. qlite)"
	usage
	exit 1
fi

if [[ -z $2 ]]
then
	echo "missing version number as second parameter"
	echo "last version was:"
	cat last_export_ver.$1 2>/dev/null
	usage
	exit 1
fi

pkg_name=$1
ver="$2"
name_ver=${pkg_name}_${ver}
pkg_list=${pkg_name}.exlist

if [[ ! -r $pkg_list ]]
then
	echo "unable to find export list: $pkg_list"
	exit 1
fi

if (( ! rebuild ))
then
	echo $ver >last_export_ver.$1
fi

if (( build ))
then
	build_it
fi


if [[ -z $dir ]]
then
	#dir=$ex_root/$(date +%Y%m%d)
	dir=$ex_root/$name_ver
fi
if [[ ! -d $dir ]]
then
	if ! mkdir -p $dir
	then
		echo "cannot make export dir: $dir"
		exit 1
	else
		verbose "made export dir: $dir"
	fi
fi

verbose "populating..."
sed 's/#.*//; /^$/d' $pkg_list >/tmp/PID$$.data		# snarf the list and strip comments/blank lines

typeset -A seen
# copy things from the list into the export directory
trap "echo something failed!!!!" EXIT
set -e
compress=""
mode=""
while read src target mode compress junk
do
	if [[ -z ${seen[${target%/*}]} ]]			# ensure that the directory exists
	then
		verbose "ensuring $dir/${target%/*} exists"
		mkdir -p $dir/${target%/*}
		seen[${target%/*}]="true"
	fi

	if [[ ! -f $src ]]							# possibly foo given for foo.ksh, foo.bsh or foo.py
	then
		for x in .ksh .sh .bsh .py				# assume all other files must retain extension for their interpreter to work
		do
			if [[ -f "$src$x" ]]
			then
				if [[ $target == *"/" ]]			# ensure non-suffixed name ends up in target dir
				then	
					target+="${src##*/}"
				fi
				src+=$x
				break
			fi
		done
	fi
	verbose "$src -> $dir/${target} (${mode:-755}, ${compress:-no-compression})"
	if cp $src $dir/$target
	then

		if [[ -z $mode || $mode == "-" ]]
		then
			mode="775"
		fi
		if [[ $target == *"/" ]]
		then
			ctarget=$dir/$target/${src##*/}
		else
			ctarget=$dir/$target
		fi

		chmod $mode $ctarget
		if [[ -n $compress ]]
		then
			echo "compressing: $compress $compress_options $ctarget"
			$compress $compress_options $ctarget
		fi


	fi
done </tmp/PID$$.data
verbose ""


rm -f /tmp/PID$$.*


if ! cd $dir
then
	echo "cannot cd to export dir ($dir) to create tar file"
	exit
fi

mkdir -p $ex_root/bundle/
tar -cf - . | gzip >/$ex_root/bundle/attlr${pkg_name}-${ver}.tar.gz
trap - EXIT
echo "packaged ready for deb build in: $ex_root/bundle/attlr${pkg_name}-${ver}.tar.gz"
