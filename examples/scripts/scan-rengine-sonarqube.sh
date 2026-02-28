#!/bin/bash
# scan-rengine-sonarqube.sh - Scan rengine project with SonarQube
set -euo pipefail

PROJECT_DIR="/root/rengine"
SONAR_HOST="http://localhost:9000"
SONAR_TOKEN="squ_38a0d0223a67b10f9eade37e84c468085323349a"

echo "=== Rengine SonarQube Scan ==="
cd "$PROJECT_DIR"

# Step 1: Compile modules with jacoco
echo ">> Compiling modules..."
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk-amd64
mvn -B clean compile -DskipTests -pl common,apiserver,controller,service > /dev/null 2>&1 || {
    echo "WARNING: Not all modules compiled. Scanning available ones..."
}

# Step 2: Determine which modules have compiled binaries
declare -a SOURCES=()
declare -a BINARIES=()
declare -a TESTS=()

for mod in common apiserver controller executor service job; do
    if [ -d "$PROJECT_DIR/$mod/src/main/java" ]; then
        SOURCES+=("$mod/src/main/java")
        BINARIES+=("$mod/target/classes")
    fi
    if [ -d "$mod/src/test/java" ]; then
        TESTS+=("$mod/src/test/java")
    fi
done

if [ ${#SOURCES[@]} -eq 0 ]; then
    echo "ERROR: No compilable modules found"
    exit 1
fi

IFS=, SRCS="${SOURCES[*]}"
IFS=, BINS="${BINARIES[*]}"
IFS=, TSTS="${TESTS[*]}"

echo ">> Sources: $SRCS"
echo ">> Binaries: $BINS"

# Step 3: Run SonarScanner
echo ">> Running SonarScanner..."
sonar-scanner \
  -Dsonar.host.url="$SONAR_HOST" \
  -Dsonar.login="$SONAR_TOKEN" \
  -Dsonar.projectKey=rengine \
  -Dsonar.projectName=Rengine \
  -Dsonar.projectVersion=1.0.0 \
  -Dsonar.sources="$SRCS" \
  -Dsonar.java.binaries="$BINS" \
  -Dsonar.tests="$TSTS" \
  -Dsonar.java.coveragePlugin=jacoco \
  -Dsonar.coverage.jacoco.xmlReportPaths="**/target/site/jacoco/jacoco.xml" \
  -Dsonar.sourceEncoding=UTF-8 \
  -Dsonar.java.source=11

echo "=== Scan complete! ==="
echo "Results: $SONAR_HOST/dashboard?id=rengine"
