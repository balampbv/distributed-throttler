# Controlled Throughput Benchmark Results

This document contains the results of the controlled throughput benchmark, which runs for 1 hour with a target throughput of 500,000 operations per minute.

## Benchmark Configuration

- **Target Throughput**: 500,000 operations per minute (8,333 ops/second)
- **Duration**: 1 hour
- **Number of Workers**: 20
- **Rate Limit Window**: 1 minute
- **Wait Strategy**: Clients wait when the throughput limit is reached

## Benchmark Results

### Operations Statistics

- **Total Operations**: [TOTAL_OPS]
- **Throttled Operations**: [THROTTLED_OPS]
- **Overall Throughput**: [OPS_PER_SECOND] ops/second ([OPS_PER_MINUTE] ops/minute)
- **Percentage of Target**: [PERCENTAGE]%

### Redis Resource Utilization

#### CPU Usage

- **Average CPU Usage**: [AVG_CPU]%
- **Peak CPU Usage**: [PEAK_CPU]%
- **CPU Usage Over Time**: [Insert chart or description]

#### Memory Usage

- **Average Memory Usage**: [AVG_MEM] MB
- **Peak Memory Usage**: [PEAK_MEM] MB
- **Memory Usage Over Time**: [Insert chart or description]

#### Network I/O

- **Total Network In**: [TOTAL_NET_IN] MB
- **Total Network Out**: [TOTAL_NET_OUT] MB
- **Average Network In**: [AVG_NET_IN] MB/s
- **Average Network Out**: [AVG_NET_OUT] MB/s

## Analysis

### Throughput Control

[Describe how well the throttler maintained the target throughput of 500K/min. Include observations about any variations or patterns in the throughput over time.]

### Client Waiting Behavior

[Describe how clients behaved when the throughput limit was reached. Include observations about wait times, throttled operations, and any patterns in client behavior.]

### Redis Performance

[Analyze Redis performance during the benchmark. Include observations about CPU and memory utilization, and how they correlate with throughput.]

## Conclusion

[Summarize the overall performance of the throttler in maintaining a controlled throughput of 500K/min for 1 hour. Include any recommendations for improving performance or reliability.]

## How to Generate Charts

You can generate charts from the Redis metrics CSV file using tools like Excel, Google Sheets, or Python with matplotlib. Here's a simple Python script to generate charts:

```python
import pandas as pd
import matplotlib.pyplot as plt
import seaborn as sns

# Load the data
df = pd.read_csv('benchmark_results/redis_metrics_YYYYMMDD_HHMMSS.csv')

# Convert timestamp to datetime
df['Timestamp'] = pd.to_datetime(df['Timestamp'])

# Set up the figure
plt.figure(figsize=(12, 10))

# CPU Usage over time
plt.subplot(3, 1, 1)
plt.plot(df['Timestamp'], df['CPU %'])
plt.title('Redis CPU Usage Over Time')
plt.ylabel('CPU %')
plt.grid(True)

# Memory Usage over time
plt.subplot(3, 1, 2)
plt.plot(df['Timestamp'], df['Memory Usage (MB)'])
plt.title('Redis Memory Usage Over Time')
plt.ylabel('Memory (MB)')
plt.grid(True)

# Network I/O over time
plt.subplot(3, 1, 3)
plt.plot(df['Timestamp'], df['Network In (MB)'], label='In')
plt.plot(df['Timestamp'], df['Network Out (MB)'], label='Out')
plt.title('Redis Network I/O Over Time')
plt.ylabel('Network (MB)')
plt.legend()
plt.grid(True)

plt.tight_layout()
plt.savefig('benchmark_results/redis_metrics_chart.png')
plt.show()
```

Replace 'benchmark_results/redis_metrics_YYYYMMDD_HHMMSS.csv' with the actual path to your metrics file.