if (ACTION != 'Rollback') {
    return []
}

def cmd = """
curl -s -u 'dnps:SXKvkwRICe743VPhTkKIbA==' \
  'https://artifactory.gainetics.io/artifactory/observe-dnps/prod-ipdb-manager/' \
| perl -ne 'if (/href="([^"]+\.tar\.gz)".*?(\d{2})-([A-Za-z]{3})-(\d{4})\s+(\d{2}):(\d{2})/) { %m=(Jan=>1,Feb=>2,Mar=>3,Apr=>4,May=>5,Jun=>6,Jul=>7,Aug=>8,Sep=>9,Oct=>10,Nov=>11,Dec=>12); printf "%04d%02d%02d%02d%02d\t%s\n",\$4,\$m{\$3},\$2,\$5,\$6,\$1 }' \
| sort -r \
| head -n 10 \
| cut -f2
"""

def proc = ["bash", "-lc", cmd].execute()
proc.waitFor()

if (proc.exitValue() != 0) {
    return ["ERROR: failed to fetch artifacts"]
}

def lines = proc.in.text.readLines().findAll { it?.trim() }
return lines ?: ["NO_ARTIFACT_FOUND"]
