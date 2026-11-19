# External evaluation data

This directory keeps the download script and the checksum. It does not keep
the dataset.

`download.sh` fetches LongMemEval oracle (500 questions, evidence sessions
only) from the cleaned Hugging Face release and checks SHA-256 before renaming
the file to `longmemeval_oracle.json`. That JSON is gitignored.

```bash
sh eval/datasets/download.sh
```

Source: [xiaowu0162/longmemeval-cleaned](https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned),
file `longmemeval_oracle.json`, MIT license. The pinned digest is in
`checksums.txt`.

`fixture.json` is a two-question stand-in used by `go test`. It is not
LongMemEval and it is not a score.

`memorigo-eval` still runs the builtin 16 scenarios when the oracle file is
absent, prints `external dataset not present`, and exits from those scenarios
alone. `-external-limit N` samples N oracle questions with heuristic extraction
and checks whether the answer string appears in the packed context. That sample
is not a LongMemEval score, not an LLM-judge score, and it does not change the
builtin pass/fail line.

`MEMORI_EVAL_JUDGE` defaults to off. A URL (or `http` plus
`MEMORI_EVAL_JUDGE_URL`) posts `{question, expected, hypothesis}` and reads
`{"score": 0..1}`. The score is reported beside the suite. It does not change
`Passed`, and the memory write path never calls it.
