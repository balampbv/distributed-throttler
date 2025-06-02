#!/bin/bash

# Function to show usage
show_usage() {
  echo "Usage: $0 [options]"
  echo "Options:"
  echo "  --all                Run all benchmarks"
  echo "  --high-throughput    Run high throughput benchmark"
  echo "  --controlled         Run controlled throughput benchmark (1 hour)"
  echo "  --help               Show this help message"
  echo ""
  echo "If no options are provided, --all is assumed."
}

# Parse command line arguments
RUN_ALL=false
RUN_HIGH_THROUGHPUT=false
RUN_CONTROLLED=false

if [ $# -eq 0 ]; then
  RUN_ALL=true
else
  for arg in "$@"; do
    case $arg in
      --all)
        RUN_ALL=true
        ;;
      --high-throughput)
        RUN_HIGH_THROUGHPUT=true
        ;;
      --controlled)
        RUN_CONTROLLED=true
        ;;
      --help)
        show_usage
        exit 0
        ;;
      *)
        echo "Unknown option: $arg"
        show_usage
        exit 1
        ;;
    esac
  done
fi

# Ensure Redis is running
echo "Checking if Redis is running..."
if ! redis-cli ping > /dev/null 2>&1; then
  echo "Redis is not running. Starting Redis using Docker Compose..."
  docker-compose up -d

  # Wait for Redis to be ready
  echo "Waiting for Redis to be ready..."
  for i in {1..10}; do
    if redis-cli ping > /dev/null 2>&1; then
      echo "Redis is ready!"
      break
    fi
    if [ $i -eq 10 ]; then
      echo "Redis failed to start. Please check your Docker and Redis configuration."
      exit 1
    fi
    echo "Waiting for Redis to start... ($i/10)"
    sleep 2
  done
fi

# Create results directory
mkdir -p benchmark_results

# Function to monitor Redis CPU and memory usage
monitor_redis() {
  local output_file=$1
  local interval=${2:-5}  # Default to 5 seconds
  local container_id=$(docker ps -qf "name=redis")

  if [ -z "$container_id" ]; then
    echo "Redis container not found. Monitoring disabled."
    return 1
  fi

  echo "Starting Redis monitoring (container: $container_id, interval: ${interval}s)"
  echo "Timestamp,CPU %,Memory Usage (MB),Memory Limit (MB),Memory %,Network In (MB),Network Out (MB)" > "$output_file"

  while true; do
    # Get stats from Docker
    stats=$(docker stats --no-stream --format "{{.CPUPerc}},{{.MemUsage}},{{.NetIO}}" "$container_id" 2>/dev/null || echo "0%,0MiB / 0MiB,0B / 0B")

    # Parse the stats
    cpu_perc=$(echo $stats | cut -d',' -f1 | sed 's/%//')
    mem_usage=$(echo $stats | cut -d',' -f2 | sed 's/\([0-9.]\+\)\([A-Za-z]\+\) \/ \([0-9.]\+\)\([A-Za-z]\+\)/\1,\3/')
    mem_used=$(echo $mem_usage | cut -d',' -f1)
    mem_limit=$(echo $mem_usage | cut -d',' -f2)

    # Convert to MB if needed
    if [[ $mem_used == *"GiB"* ]]; then
      mem_used=$(echo $mem_used | sed 's/GiB//' | awk '{print $1 * 1024}' 2>/dev/null || echo "0")
    elif [[ $mem_used == *"MiB"* ]]; then
      mem_used=$(echo $mem_used | sed 's/MiB//' 2>/dev/null || echo "0")
    elif [[ $mem_used == *"KiB"* ]]; then
      mem_used=$(echo $mem_used | sed 's/KiB//' | awk '{print $1 / 1024}' 2>/dev/null || echo "0")
    else
      mem_used="0"
    fi

    if [[ $mem_limit == *"GiB"* ]]; then
      mem_limit=$(echo $mem_limit | sed 's/GiB//' | awk '{print $1 * 1024}' 2>/dev/null || echo "0")
    elif [[ $mem_limit == *"MiB"* ]]; then
      mem_limit=$(echo $mem_limit | sed 's/MiB//' 2>/dev/null || echo "0")
    elif [[ $mem_limit == *"KiB"* ]]; then
      mem_limit=$(echo $mem_limit | sed 's/KiB//' | awk '{print $1 / 1024}' 2>/dev/null || echo "0")
    else
      mem_limit="0"
    fi

    # Calculate memory percentage
    if [[ -n "$mem_used" && -n "$mem_limit" && "$mem_limit" != "0" && "$mem_limit" != "0.0" ]]; then
      # Use bc for safer floating-point arithmetic
      mem_perc=$(echo "scale=2; $mem_used / $mem_limit * 100" | bc 2>/dev/null || echo "0")
    else
      mem_perc="0"
    fi

    # Parse network I/O
    net_io=$(echo $stats | cut -d',' -f3)
    net_in=$(echo $net_io | awk '{print $1}' 2>/dev/null || echo "0B")
    net_out=$(echo $net_io | awk '{print $3}' 2>/dev/null || echo "0B")

    # Convert to MB if needed
    if [[ $net_in == *"GB"* ]]; then
      net_in=$(echo $net_in | sed 's/GB//' | awk '{print $1 * 1024}' 2>/dev/null || echo "0")
    elif [[ $net_in == *"MB"* ]]; then
      net_in=$(echo $net_in | sed 's/MB//' 2>/dev/null || echo "0")
    elif [[ $net_in == *"kB"* ]]; then
      net_in=$(echo $net_in | sed 's/kB//' | awk '{print $1 / 1024}' 2>/dev/null || echo "0")
    elif [[ $net_in == *"B"* ]]; then
      net_in=$(echo $net_in | sed 's/B//' | awk '{print $1 / (1024*1024)}' 2>/dev/null || echo "0")
    else
      net_in="0"
    fi

    if [[ $net_out == *"GB"* ]]; then
      net_out=$(echo $net_out | sed 's/GB//' | awk '{print $1 * 1024}' 2>/dev/null || echo "0")
    elif [[ $net_out == *"MB"* ]]; then
      net_out=$(echo $net_out | sed 's/MB//' 2>/dev/null || echo "0")
    elif [[ $net_out == *"kB"* ]]; then
      net_out=$(echo $net_out | sed 's/kB//' | awk '{print $1 / 1024}' 2>/dev/null || echo "0")
    elif [[ $net_out == *"B"* ]]; then
      net_out=$(echo $net_out | sed 's/B//' | awk '{print $1 / (1024*1024)}' 2>/dev/null || echo "0")
    else
      net_out="0"
    fi

    # Get timestamp
    timestamp=$(date +"%Y-%m-%d %H:%M:%S")

    # Write to file
    echo "$timestamp,$cpu_perc,$mem_used,$mem_limit,$mem_perc,$net_in,$net_out" >> "$output_file"

    sleep $interval
  done
}

# Run all benchmarks
if [ "$RUN_ALL" = true ]; then
  echo "Running all benchmarks..."
  echo "This may take a few minutes..."

  # Run all benchmarks and save the results (skip tests)
  go test -bench=. -benchmem -run=^$ ./throttler | tee benchmark_results/all_benchmarks.txt
fi

# Run high throughput benchmark
if [ "$RUN_ALL" = true ] || [ "$RUN_HIGH_THROUGHPUT" = true ]; then
  echo "Running high throughput benchmark with more iterations..."
  go test -bench=BenchmarkHighThroughput -benchtime=10s -run=^$ ./throttler | tee benchmark_results/high_throughput.txt
fi

# Run controlled throughput benchmark
if [ "$RUN_ALL" = true ] || [ "$RUN_CONTROLLED" = true ]; then
  echo "Running controlled throughput benchmark (1 hour)..."
  echo "This will take 1 hour to complete."
  echo "Redis CPU and memory usage will be monitored during the benchmark."

  # Start Redis monitoring in the background
  monitor_file="benchmark_results/redis_metrics_$(date +%Y%m%d_%H%M%S).csv"
  monitor_redis "$monitor_file" 10 &
  monitor_pid=$!

  # Run the benchmark
  go test -bench=BenchmarkControlledThroughput -benchtime=1x -run=^$ ./throttler | tee benchmark_results/controlled_throughput.txt

  # Stop the monitoring
  kill $monitor_pid

  echo "Redis monitoring data saved to $monitor_file"
fi

echo "Benchmarks completed. Results saved to benchmark_results directory."
if [ "$RUN_ALL" = true ]; then
  echo "You can view the results with: cat benchmark_results/all_benchmarks.txt"
fi
if [ "$RUN_HIGH_THROUGHPUT" = true ] || [ "$RUN_ALL" = true ]; then
  echo "You can view high throughput results with: cat benchmark_results/high_throughput.txt"
fi
if [ "$RUN_CONTROLLED" = true ] || [ "$RUN_ALL" = true ]; then
  echo "You can view controlled throughput results with: cat benchmark_results/controlled_throughput.txt"
  echo "You can analyze Redis metrics with: cat $monitor_file"
fi
