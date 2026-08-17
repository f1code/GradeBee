-- 012_drop_report_examples.sql
-- Report Examples are superseded by Level Report Instructions (#53); the
-- example subsystem (upload, Drive import, extraction job, style-matching
-- prompt section) is removed in #54. Drop both tables — nothing else
-- references them by FK.

DROP TABLE IF EXISTS report_example_classes;
DROP TABLE IF EXISTS report_examples;
