# echo "$(date '+%Y-%m-%d %H:%M:%S') [INFO] Deploying Start" >> ./deploy.log

DB_NAME="sciedu-local-postgres-1"
LDAP_NAME="sciedu-local-ldap-1"

if ! docker compose ps ${DB_NAME} | grep -q "running"; then
    echo "Database not running. Starting..."
    docker start ${DB_NAME}
else
    echo "Database already running."
fi
